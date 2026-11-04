package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/syberfoxx/gamehost/internal/backup"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// newScheduledInstance creates one Core Keeper instance with a backup schedule.
func newScheduledInstance(t *testing.T, s *store.Store, every string) instance.Instance {
	t.Helper()

	layout := s.Layout("corekeeper")
	inst, err := instance.New("corekeeper", instance.Options{
		MaxPlayers:  4,
		Mode:        instance.ModeNormal,
		BackupEvery: every,
		InstallDir:  layout.Files,
		DataDir:     layout.Data,
	})
	if err != nil {
		t.Fatalf("instance.New() error = %v", err)
	}
	if err := s.Create(inst); err != nil {
		t.Fatalf("store.Create() error = %v", err)
	}
	return inst
}

// covers a due backup of a stopped instance and the interval in between
func TestScheduledBackup(t *testing.T) {
	t.Parallel()

	s := store.New(t.TempDir())
	inst := newScheduledInstance(t, s, "1h")
	if err := os.WriteFile(filepath.Join(inst.DataDir, "ServerConfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	now := time.Now()
	sched := &backupScheduler{
		store:   s,
		out:     &out,
		started: now.Add(-2 * time.Hour),
		now:     func() time.Time { return now },
		stop:    func(instance.Instance) error { return nil },
		start:   func(instance.Instance) error { return nil },
	}

	if err := sched.maybeBackup(inst); err != nil {
		t.Fatalf("maybeBackup() error = %v", err)
	}
	archives, err := backup.List(s.Layout(inst.Name).Backups)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("archives = %v, want one scheduled backup", archives)
	}
	if !strings.Contains(out.String(), "scheduled backup of corekeeper") {
		t.Errorf("output = %q, want the backup notice", out.String())
	}

	// The archive just written is the new reference: the next tick is not due.
	if err := sched.maybeBackup(inst); err != nil {
		t.Fatalf("second maybeBackup() error = %v", err)
	}
	if archives, err = backup.List(s.Layout(inst.Name).Backups); err != nil || len(archives) != 1 {
		t.Errorf("archives = %v, %v; want the same single archive", archives, err)
	}
}

// covers a disabled schedule doing nothing
func TestScheduledBackupDisabled(t *testing.T) {
	t.Parallel()

	s := store.New(t.TempDir())
	inst := newScheduledInstance(t, s, "")

	sched := &backupScheduler{
		store:   s,
		out:     &bytes.Buffer{},
		started: time.Now().Add(-48 * time.Hour),
		now:     time.Now,
		stop:    func(instance.Instance) error { return nil },
		start:   func(instance.Instance) error { return nil },
	}
	if err := sched.maybeBackup(inst); err != nil {
		t.Fatalf("maybeBackup() error = %v", err)
	}
	if archives, err := backup.List(s.Layout(inst.Name).Backups); err != nil || len(archives) != 0 {
		t.Errorf("archives = %v, %v; want none", archives, err)
	}
}

// covers stopping a server for its backup and starting it again
func TestScheduledBackupRestartsServer(t *testing.T) {
	t.Parallel()

	s := store.New(t.TempDir())
	inst := newScheduledInstance(t, s, "1h")
	if err := s.SaveState(inst.Name, instance.State{
		Phase:     instance.PhaseRunning,
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	now := time.Now()
	stopped, started := 0, 0
	sched := &backupScheduler{
		store:   s,
		out:     &out,
		started: now.Add(-2 * time.Hour),
		now:     func() time.Time { return now },
		stop: func(i instance.Instance) error {
			stopped++
			// The supervisor records the terminal phase after a graceful stop.
			return s.SaveState(i.Name, instance.State{Phase: instance.PhaseStopped})
		},
		start: func(instance.Instance) error {
			started++
			return nil
		},
	}

	if err := sched.maybeBackup(inst); err != nil {
		t.Fatalf("maybeBackup() error = %v", err)
	}
	if stopped != 1 {
		t.Errorf("stop calls = %d, want the server stopped for the archive", stopped)
	}
	if started != 1 {
		t.Errorf("start calls = %d, want the server started again", started)
	}
	if !strings.Contains(out.String(), "stopping the server for a clean archive") {
		t.Errorf("output = %q, want the maintenance notice", out.String())
	}
}

// covers a restart even when the archive could not be written
func TestScheduledBackupRestartsAfterFailure(t *testing.T) {
	t.Parallel()

	s := store.New(t.TempDir())
	inst := newScheduledInstance(t, s, "1h")
	if err := s.SaveState(inst.Name, instance.State{
		Phase: instance.PhaseRunning,
		PID:   os.Getpid(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(inst.DataDir, "ServerConfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A read-only backup directory makes the archive fail after the stop.
	backups := s.Layout(inst.Name).Backups
	if err := os.Chmod(backups, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(backups, 0o755) })

	started := 0
	now := time.Now()
	sched := &backupScheduler{
		store:   s,
		out:     &bytes.Buffer{},
		started: now.Add(-2 * time.Hour),
		now:     func() time.Time { return now },
		stop: func(i instance.Instance) error {
			return s.SaveState(i.Name, instance.State{Phase: instance.PhaseStopped})
		},
		start: func(instance.Instance) error {
			started++
			return nil
		},
	}

	if err := sched.maybeBackup(inst); err == nil {
		t.Error("maybeBackup() error = nil, want the failed archive")
	}
	if started != 1 {
		t.Errorf("start calls = %d, want the server restarted anyway", started)
	}
}
