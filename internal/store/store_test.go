package store

import (
	"errors"
	"os"
	"testing"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// newInstance builds a valid instance rooted in a test store.
func newInstance(t *testing.T, s *Store, name string) instance.Instance {
	t.Helper()

	layout := s.Layout(name)
	inst, err := instance.New(name, instance.Options{
		MaxPlayers: 5,
		Mode:       instance.ModeNormal,
		InstallDir: layout.Files,
		DataDir:    layout.Data,
	})
	if err != nil {
		t.Fatalf("instance.New() error = %v", err)
	}
	return inst
}

// covers create, layout, load and duplicate detection
func TestStoreCreateLoad(t *testing.T) {
	t.Parallel()

	s := New(t.TempDir())
	inst := newInstance(t, s, "corekeeper")

	if err := s.Create(inst); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for _, dir := range []string{inst.InstallDir, inst.DataDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %q: %v", dir, err)
		}
		if !info.IsDir() {
			t.Errorf("%q is not a directory", dir)
		}
	}

	got, err := s.Load(inst.Name)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Name != inst.Name || got.MaxPlayers != 5 || got.Network.Mode != instance.NetworkSDR {
		t.Errorf("Load() = %+v, want the created instance", got)
	}

	if err := s.Create(inst); !errors.Is(err, ErrExists) {
		t.Errorf("second Create() error = %v, want ErrExists", err)
	}
}

// covers the missing instance path
func TestStoreLoadNotFound(t *testing.T) {
	t.Parallel()

	s := New(t.TempDir())
	if _, err := s.Load("ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Load() error = %v, want ErrNotFound", err)
	}
}

// covers listing and ordering
func TestStoreList(t *testing.T) {
	t.Parallel()

	s := New(t.TempDir())
	if got, err := s.List(); err != nil || got != nil {
		t.Fatalf("List() = %v, %v; want empty", got, err)
	}

	for _, name := range []string{"zeta", "alpha"} {
		if err := s.Create(newInstance(t, s, name)); err != nil {
			t.Fatalf("Create(%q) error = %v", name, err)
		}
	}

	got, err := s.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Errorf("List() = %+v, want alpha then zeta", got)
	}
}
