package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// dstWorldInstance creates a stopped DST instance with Caves and returns its home.
func dstWorldInstance(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GAMEHOST_HOME", home)
	if _, err := runArgs(t, "create", "cave", "--game", "dontstarvetogether",
		"--offline", "--caves", "--world-name", "Owls"); err != nil {
		t.Fatalf("create: %v", err)
	}
	return home
}

// dstSavePath returns the generated save folder of one shard in the test home.
func dstSavePath(home, shard string) string {
	return filepath.Join(home, "servers", "cave", "data", "gamehost", "cave", shard, "save")
}

func writeWorldMarkers(t *testing.T, home string) (master, caves, clusterIni string) {
	t.Helper()
	master = filepath.Join(dstSavePath(home, "Master"), "world")
	caves = filepath.Join(dstSavePath(home, "Caves"), "world")
	clusterIni = filepath.Join(home, "servers", "cave", "data", "gamehost", "cave", "cluster.ini")
	for _, path := range []string{master, caves, clusterIni} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("sentinel"), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}
	return master, caves, clusterIni
}

func TestWorldRecreateNeedsConfirmation(t *testing.T) {
	home := dstWorldInstance(t)
	master, caves, _ := writeWorldMarkers(t, home)

	if _, err := runArgs(t, "world", "recreate", "cave"); !errors.Is(err, ErrUsage) {
		t.Fatalf("recreate without --yes: %v, want ErrUsage", err)
	}
	for _, path := range []string{master, caves} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("unconfirmed recreate removed %q: %v", path, err)
		}
	}
}

func TestWorldRecreateArchivesAndRemovesSaves(t *testing.T) {
	home := dstWorldInstance(t)
	master, caves, clusterIni := writeWorldMarkers(t, home)

	out, err := runArgs(t, "world", "recreate", "cave", "--yes")
	if err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if !strings.Contains(out, "previous data saved as") {
		t.Errorf("recreate output lacks the safety archive:\n%s", out)
	}
	for _, path := range []string{master, caves} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("save %q still exists (err = %v)", path, err)
		}
	}
	if _, err := os.Stat(clusterIni); err != nil {
		t.Errorf("recreate removed the cluster configuration: %v", err)
	}

	backups := filepath.Join(home, "servers", "cave", "backups")
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatalf("read backups: %v", err)
	}
	var found bool
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "pre-recreate_") {
			found = true
		}
	}
	if !found {
		t.Errorf("no pre-recreate_ safety archive in %v", entries)
	}
}

func TestWorldRecreateRefusesRunningInstance(t *testing.T) {
	dstWorldInstance(t)
	s := store.New(os.Getenv("GAMEHOST_HOME"))
	state := instance.State{Phase: instance.PhaseRunning, PID: os.Getpid(), StartedAt: time.Now()}
	if err := s.SaveState("cave", state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if _, err := runArgs(t, "world", "recreate", "cave", "--yes"); !errors.Is(err, ErrRunning) {
		t.Fatalf("recreate running instance: %v, want ErrRunning", err)
	}
}

func TestWorldRecreateRefusesOtherGames(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())
	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := runArgs(t, "world", "recreate", "corekeeper", "--yes"); !errors.Is(err, ErrUsage) {
		t.Fatalf("recreate corekeeper: %v, want ErrUsage", err)
	}
}

func TestWorldRollbackRestoresAnArchive(t *testing.T) {
	home := dstWorldInstance(t)
	_, _, clusterIni := writeWorldMarkers(t, home)
	if err := os.WriteFile(clusterIni, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runArgs(t, "backup", "cave"); err != nil {
		t.Fatalf("backup: %v", err)
	}

	backups := filepath.Join(home, "servers", "cave", "backups")
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatalf("read backups: %v", err)
	}
	var archive string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tar.gz") {
			archive = entry.Name()
			break
		}
	}
	if archive == "" {
		t.Fatal("backup created no archive")
	}

	if err := os.WriteFile(clusterIni, []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runArgs(t, "world", "rollback", "cave", archive); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	data, err := os.ReadFile(clusterIni)
	if err != nil {
		t.Fatalf("read restored file: %v", err)
	}
	if string(data) != "original" {
		t.Errorf("restored cluster.ini = %q, want original", data)
	}
}
