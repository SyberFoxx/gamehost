// Package store persists instances on disk.
package store

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/syberfoxx/gamehost/internal/atomicfile"
	"github.com/syberfoxx/gamehost/internal/instance"
)

const (
	instanceFile = "instance.json"
	stateFile    = "state.json"
	lockFile     = "run.lock"
)

var (
	// ErrExists reports an instance whose directory is already present.
	ErrExists = errors.New("instance already exists")
	// ErrNotFound reports an instance that does not exist.
	ErrNotFound = errors.New("instance not found")
	// ErrLocked reports an instance another process is already using.
	ErrLocked = errors.New("instance is in use")
)

// Layout holds the directories owned by one instance.
type Layout struct {
	Root    string
	Files   string
	Data    string
	Logs    string
	Backups string
}

// Store owns the on-disk state below its root.
type Store struct {
	root string
}

// New returns a store rooted at root.
func New(root string) *Store {
	return &Store{root: root}
}

// Root returns the directory every instance lives under.
func (s *Store) Root() string {
	return s.root
}

// Layout derives the directory layout of one instance.
func (s *Store) Layout(name string) Layout {
	dir := filepath.Join(s.root, "servers", name)
	return Layout{
		Root:    dir,
		Files:   filepath.Join(dir, "files"),
		Data:    filepath.Join(dir, "data"),
		Logs:    filepath.Join(dir, "logs"),
		Backups: filepath.Join(dir, "backups"),
	}
}

// Create writes a new instance and its directories.
func (s *Store) Create(inst instance.Instance) error {
	layout := s.Layout(inst.Name)

	switch _, err := os.Stat(layout.Root); {
	case err == nil:
		return fmt.Errorf("create instance %q: %w", inst.Name, ErrExists)
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("inspect instance %q: %w", inst.Name, err)
	}

	for _, dir := range []string{layout.Files, layout.Data, layout.Logs, layout.Backups} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %q: %w", dir, err)
		}
	}
	return s.Save(inst)
}

// Save overwrites the configuration of an existing instance.
func (s *Store) Save(inst instance.Instance) error {
	data, err := json.MarshalIndent(inst, "", "  ")
	if err != nil {
		return fmt.Errorf("encode instance %q: %w", inst.Name, err)
	}
	data = append(data, '\n')

	path := s.configPath(inst.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create instance directory: %w", err)
	}
	// The configuration can hold a cluster token, so it stays private.
	if err := atomicfile.Replace(path, data, 0o600); err != nil {
		return fmt.Errorf("save instance %q: %w", inst.Name, err)
	}
	return nil
}

// Load reads and validates one instance.
func (s *Store) Load(name string) (instance.Instance, error) {
	path := s.configPath(name)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return instance.Instance{}, fmt.Errorf("load instance %q: %w", name, ErrNotFound)
		}
		return instance.Instance{}, fmt.Errorf("read %q: %w", path, err)
	}

	var inst instance.Instance
	if err := json.Unmarshal(data, &inst); err != nil {
		return instance.Instance{}, fmt.Errorf("decode %q: %w", path, err)
	}
	if err := inst.Validate(); err != nil {
		return instance.Instance{}, fmt.Errorf("load instance %q: %w", name, err)
	}
	return inst, nil
}

// List returns every stored instance sorted by name.
func (s *Store) List() ([]instance.Instance, error) {
	dir := filepath.Join(s.root, "servers")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list instances: %w", err)
	}

	var instances []instance.Instance
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		inst, err := s.Load(entry.Name())
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}

	slices.SortFunc(instances, func(a, b instance.Instance) int {
		return cmp.Compare(a.Name, b.Name)
	})
	return instances, nil
}

// Lock takes the exclusive run lock of one instance. Only one server or one
// data-changing command per instance may hold it at a time.
// The kernel drops the lock when the holder exits, so a crash leaves no stale
// lock behind and the file itself carries no state.
func (s *Store) Lock(name string) (*Lock, error) {
	path := filepath.Join(s.Layout(name).Root, lockFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open lock %q: %w", path, err)
	}
	// Non-blocking: the caller decides what a busy instance means.
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("lock instance %q: %w", name, ErrLocked)
		}
		return nil, fmt.Errorf("lock instance %q: %w", name, err)
	}
	return &Lock{file: file}, nil
}

// Lock is a held run lock. Release must be called once; the zero value is safe.
type Lock struct {
	file *os.File
}

// Release drops the lock. It is idempotent.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}
	return nil
}

// configPath returns the configuration file of one instance.
func (s *Store) configPath(name string) string {
	return filepath.Join(s.Layout(name).Root, instanceFile)
}

// SaveState writes the runtime state of one instance.
func (s *Store) SaveState(name string, state instance.State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state of %q: %w", name, err)
	}
	data = append(data, '\n')

	path := filepath.Join(s.Layout(name).Root, stateFile)
	if err := atomicfile.Replace(path, data, 0o644); err != nil {
		return fmt.Errorf("save state of %q: %w", name, err)
	}
	return nil
}

// LoadState reads the runtime state of one instance.
// A missing state file means the server was never started, so it reports stopped.
func (s *Store) LoadState(name string) (instance.State, error) {
	path := filepath.Join(s.Layout(name).Root, stateFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return instance.State{Phase: instance.PhaseStopped}, nil
		}
		return instance.State{}, fmt.Errorf("read %q: %w", path, err)
	}

	var state instance.State
	if err := json.Unmarshal(data, &state); err != nil {
		return instance.State{}, fmt.Errorf("decode %q: %w", path, err)
	}
	return state, nil
}
