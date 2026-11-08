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

// writeArchive writes an archive with a fixed modification time.
func writeArchive(t *testing.T, dir string, at time.Time) {
	t.Helper()

	path := filepath.Join(dir, at.Format("2006-01-02_15-04-05")+".tar.gz")
	if err := os.WriteFile(path, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// covers when a scheduled backup is due
func TestBackupDue(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		every   string
		started time.Time
		archive time.Time
		want    bool
	}{
		{"off", "", now.Add(-48 * time.Hour), time.Time{}, false},
		{"no archive yet, half the interval", "1h", now.Add(-30 * time.Minute), time.Time{}, false},
		{"no archive yet, past the interval", "1h", now.Add(-2 * time.Hour), time.Time{}, true},
		{"recent archive wins over the start", "1h", now.Add(-9 * time.Hour), now.Add(-30 * time.Minute), false},
		{"old archive is due", "1h", now.Add(-9 * time.Hour), now.Add(-90 * time.Minute), true},
		{"archive older than the start does not count", "1h", now.Add(-30 * time.Minute), now.Add(-5 * time.Hour), false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			s := store.New(t.TempDir())
			inst := newScheduledInstance(t, s, test.every)
			if !test.archive.IsZero() {
				writeArchive(t, s.Layout(inst.Name).Backups, test.archive)
			}

			if got := backupDue(s, inst, test.started, now); got != test.want {
				t.Errorf("backupDue() = %v, want %v", got, test.want)
			}
		})
	}
}

// covers the archive of a maintenance window and its retention
func TestArchiveWindow(t *testing.T) {
	t.Parallel()

	s := store.New(t.TempDir())
	inst := newScheduledInstance(t, s, "1h")
	inst.Backup.Keep = 1
	if err := s.Save(inst); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(inst.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst.DataDir, "ServerConfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two old archives fill the retention of one.
	writeArchive(t, s.Layout(inst.Name).Backups, time.Now().Add(-3*time.Hour))
	writeArchive(t, s.Layout(inst.Name).Backups, time.Now().Add(-2*time.Hour))

	var out bytes.Buffer
	if err := archiveWindow(s, inst, &out); err != nil {
		t.Fatalf("archiveWindow() error = %v", err)
	}

	archives, err := backup.List(s.Layout(inst.Name).Backups)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 1 {
		t.Fatalf("archives = %v, want the new one and the retention applied", archives)
	}
	if !strings.Contains(out.String(), "scheduled backup of corekeeper:") {
		t.Errorf("output = %q, want the archive notice", out.String())
	}
	if !strings.Contains(out.String(), "removed old backup") {
		t.Errorf("output = %q, want the prune notice", out.String())
	}
}

// covers a window on an instance that never wrote data
func TestArchiveWindowWithoutData(t *testing.T) {
	t.Parallel()

	s := store.New(t.TempDir())
	inst := newScheduledInstance(t, s, "1h")

	var out bytes.Buffer
	if err := archiveWindow(s, inst, &out); err != nil {
		t.Fatalf("archiveWindow() error = %v", err)
	}
	if !strings.Contains(out.String(), "skipped: no data yet") {
		t.Errorf("output = %q, want the skip notice", out.String())
	}
	if archives, err := backup.List(s.Layout(inst.Name).Backups); err != nil || len(archives) != 0 {
		t.Errorf("archives = %v, %v; want none", archives, err)
	}
}
