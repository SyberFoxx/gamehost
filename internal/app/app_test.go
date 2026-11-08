package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/process"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runArgs executes one command line and captures its output.
func runArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), args, &stdout, &stderr)
	return stdout.String(), err
}

// covers command dispatch and usage errors
func TestRun(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantOut string
		wantErr error
	}{
		{"version", []string{"version"}, "gamehost dev\n", nil},
		{"no args", nil, "", ErrUsage},
		{"unknown command", []string{"nope"}, "", ErrUsage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			err := Run(context.Background(), tt.args, &stdout, &stderr)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Run() error = %v, want %v", err, tt.wantErr)
			}
			if got := stdout.String(); got != tt.wantOut {
				t.Errorf("stdout = %q, want %q", got, tt.wantOut)
			}
			if tt.wantErr != nil && !strings.Contains(stderr.String(), "usage:") {
				t.Errorf("stderr = %q, want usage message", stderr.String())
			}
		})
	}
}

// covers the create, list and show round trip
func TestCreateListShow(t *testing.T) {
	// t.Setenv forbids t.Parallel.
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	out, err := runArgs(t, "create", "corekeeper", "--max-players", "5", "--world-name", "Cueva")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if !strings.Contains(out, `created instance "corekeeper"`) {
		t.Errorf("create output = %q", out)
	}

	out, err = runArgs(t, "list")
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	if !strings.Contains(out, "corekeeper") || !strings.Contains(out, "Cueva") {
		t.Errorf("list output = %q, want the created instance", out)
	}

	out, err = runArgs(t, "show", "corekeeper")
	if err != nil {
		t.Fatalf("show error = %v", err)
	}
	for _, want := range []string{"corekeeper", "Cueva", "sdr", "(generated on first start)"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}

	out, err = runArgs(t, "status", "corekeeper")
	if err != nil {
		t.Fatalf("status error = %v", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Errorf("status output = %q, want a stopped phase", out)
	}

	if _, err := runArgs(t, "create", "corekeeper"); !errors.Is(err, store.ErrExists) {
		t.Errorf("second create error = %v, want ErrExists", err)
	}
}

// covers status reading the game id the server wrote next to its binary
func TestStatusReadsGameID(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}

	layout := store.New(os.Getenv("GAMEHOST_HOME")).Layout("corekeeper")
	info := "No GameID found. Creating new GameID for the session.\n\nGameID: u7iGUkFwJL65k5D"
	if err := os.WriteFile(filepath.Join(layout.Files, "GameInfo.txt"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runArgs(t, "status", "corekeeper")
	if err != nil {
		t.Fatalf("status error = %v", err)
	}
	if !strings.Contains(out, "u7iGUkFwJL65k5D") {
		t.Errorf("status output missing the game id:\n%s", out)
	}
}

// covers rejected configurations
func TestCreateRejects(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	tests := []struct {
		name string
		args []string
		want error
	}{
		{"bad name", []string{"create", "../escape"}, instance.ErrInvalid},
		{"bad mode", []string{"create", "ok", "--mode", "nightmare"}, instance.ErrInvalid},
		{"too many players", []string{"create", "ok", "--max-players", "9"}, instance.ErrInvalid},
		{"missing name", []string{"create"}, ErrUsage},
		{"unknown flag", []string{"create", "ok", "--nope"}, ErrUsage},
		{"port without range", []string{"create", "ok", "--port", "70000"}, instance.ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := runArgs(t, tt.args...); !errors.Is(err, tt.want) {
				t.Errorf("%v error = %v, want %v", tt.args, err, tt.want)
			}
		})
	}
}

// covers stop when nothing is running
func TestStopWhenIdle(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}

	out, err := runArgs(t, "stop", "corekeeper")
	if err != nil {
		t.Fatalf("stop error = %v", err)
	}
	if !strings.Contains(out, "is not running") {
		t.Errorf("stop output = %q", out)
	}
}

// covers stop signalling a live server process
func TestStopSignalsRunningServer(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}

	script := filepath.Join(t.TempDir(), "server.sh")
	body := "#!/bin/sh\ntrap 'exit 0' TERM\nwhile :; do sleep 0.05; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	server, err := process.Start(process.Spec{Path: script, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("process.Start() error = %v", err)
	}
	// Reap the server like the supervisor does. An unreaped zombie keeps the
	// process group alive and makes stop wait the whole grace period.
	reaped := make(chan struct{})
	go func() {
		_ = server.WaitContext(context.Background(), stopGrace)
		close(reaped)
	}()

	s := store.New(os.Getenv("GAMEHOST_HOME"))
	state := instance.State{Phase: instance.PhaseRunning, PID: server.PID(), StartedAt: time.Now()}
	if err := s.SaveState("corekeeper", state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	out, err := runArgs(t, "stop", "corekeeper")
	if err != nil {
		t.Fatalf("stop error = %v", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Errorf("stop output = %q", out)
	}

	// The supervisor writes the final phase; stop must leave the same answer.
	got, err := s.LoadState("corekeeper")
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if got.Phase != instance.PhaseStopped || got.PID != 0 {
		t.Errorf("state = %+v, want stopped without a pid", got)
	}
	<-reaped
}

// covers refusing a second server on the same data directory
func TestStartRefusesRunningInstance(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}

	s := store.New(os.Getenv("GAMEHOST_HOME"))
	layout := s.Layout("corekeeper")
	if err := os.WriteFile(filepath.Join(layout.Files, "CoreKeeperServer"), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	sleeper, err := process.Start(process.Spec{Path: "/bin/sleep", Args: []string{"30"}})
	if err != nil {
		t.Fatalf("process.Start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = process.Stop(sleeper.PID(), time.Second)
		_ = sleeper.WaitContext(context.Background(), time.Second)
	})

	if err := s.SaveState("corekeeper", instance.State{
		Phase:     instance.PhaseRunning,
		PID:       sleeper.PID(),
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := runArgs(t, "start", "corekeeper"); !errors.Is(err, ErrRunning) {
		t.Errorf("start error = %v, want ErrRunning", err)
	}
}

// covers printing the tail of the newest log
func TestLogs(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}

	out, err := runArgs(t, "logs", "corekeeper")
	if err != nil {
		t.Fatalf("logs error = %v", err)
	}
	if !strings.Contains(out, "no logs yet") {
		t.Errorf("logs output = %q, want the empty message", out)
	}

	layout := store.New(os.Getenv("GAMEHOST_HOME")).Layout("corekeeper")
	content := "booting\nworld ready\n"
	if err := os.WriteFile(filepath.Join(layout.Logs, "2026-11-04_14-01-14.log"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err = runArgs(t, "logs", "corekeeper", "-n", "1")
	if err != nil {
		t.Fatalf("logs error = %v", err)
	}
	if out != "world ready\n" {
		t.Errorf("logs output = %q, want only the last line", out)
	}
}

// fieldValue returns the second column of the line whose first column is key.
func fieldValue(out, key string) string {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == key {
			return fields[1]
		}
	}
	return ""
}

// covers the phase transition when a stop request has no process left
func TestStatusFinishesStalePhases(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	s := store.New(os.Getenv("GAMEHOST_HOME"))

	tests := []struct {
		name  string
		phase instance.Phase
		want  string
	}{
		{"running without a process", instance.PhaseRunning, "crashed"},
		{"stopping without a process", instance.PhaseStopping, "stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := instance.State{Phase: tt.phase, PID: 0, StartedAt: time.Now()}
			if err := s.SaveState("corekeeper", state); err != nil {
				t.Fatal(err)
			}

			out, err := runArgs(t, "status", "corekeeper")
			if err != nil {
				t.Fatalf("status error = %v", err)
			}
			if got := fieldValue(out, "phase"); got != tt.want {
				t.Errorf("status phase = %q, want %q in:\n%s", got, tt.want, out)
			}
		})
	}
}

// covers a backup and restore round trip
func TestBackupAndRestore(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	s := store.New(os.Getenv("GAMEHOST_HOME"))
	layout := s.Layout("corekeeper")
	world := filepath.Join(layout.Data, "worlds", "0.world.gzip")
	if err := os.MkdirAll(filepath.Dir(world), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(world, []byte("original world"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runArgs(t, "backup", "corekeeper")
	if err != nil {
		t.Fatalf("backup error = %v", err)
	}
	if !strings.Contains(out, "backed up corekeeper") {
		t.Errorf("backup output = %q", out)
	}

	entries, err := os.ReadDir(layout.Backups)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("backups = %d, want one", len(entries))
	}
	archive := entries[0].Name()

	if err := os.WriteFile(world, []byte("corrupted"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Resolve the archive by name inside the backup directory.
	out, err = runArgs(t, "restore", "corekeeper", archive)
	if err != nil {
		t.Fatalf("restore error = %v", err)
	}
	if !strings.Contains(out, "restored corekeeper") {
		t.Errorf("restore output = %q", out)
	}

	got, err := os.ReadFile(world)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original world" {
		t.Errorf("world = %q, want the archived copy", got)
	}

	// A restore keeps the previous data as a safety archive.
	entries, err = os.ReadDir(layout.Backups)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("backups = %d, want the archive and the pre-restore copy", len(entries))
	}
}

// covers refusing backup and restore while the server is alive
func TestBackupRefusesRunningInstance(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	sleeper, err := process.Start(process.Spec{Path: "/bin/sleep", Args: []string{"30"}})
	if err != nil {
		t.Fatalf("process.Start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = process.Stop(sleeper.PID(), time.Second)
		_ = sleeper.WaitContext(context.Background(), time.Second)
	})

	s := store.New(os.Getenv("GAMEHOST_HOME"))
	if err := s.SaveState("corekeeper", instance.State{
		Phase:     instance.PhaseRunning,
		PID:       sleeper.PID(),
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"backup", "corekeeper"},
		{"restore", "corekeeper", "missing.tar.gz"},
	} {
		if _, err := runArgs(t, args...); !errors.Is(err, ErrRunning) {
			t.Errorf("%v error = %v, want ErrRunning", args, err)
		}
	}
}

// covers creating a Project Zomboid instance and its game-specific output
func TestCreateProjectZomboid(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	out, err := runArgs(t, "create", "zomboid", "--game", "projectzomboid",
		"--world-name", "base", "--admin-password", "secret")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if !strings.Contains(out, `created instance "zomboid"`) {
		t.Errorf("create output = %q", out)
	}

	out, err = runArgs(t, "list")
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	for _, want := range []string{"projectzomboid", "base", "16261/16262"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}

	out, err = runArgs(t, "show", "zomboid")
	if err != nil {
		t.Fatalf("show error = %v", err)
	}
	for _, want := range []string{"direct port", "16262", "admin password", "secret", "from the server ini"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}

	out, err = runArgs(t, "status", "zomboid")
	if err != nil {
		t.Fatalf("status error = %v", err)
	}
	for _, want := range []string{"stopped", "join", "port 16261 UDP"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
}

// covers rejecting flags that belong to the other game
func TestCreateRejectsForeignFlags(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	tests := [][]string{
		{"create", "zomboid", "--game", "projectzomboid", "--admin-password", "secret", "--max-players", "4"},
		{"create", "zomboid", "--game", "projectzomboid", "--admin-password", "secret", "--mode", "hard"},
		{"create", "corekeeper", "--admin-password", "secret"},
		{"create", "corekeeper", "--udp-port", "16262"},
	}

	for _, args := range tests {
		if _, err := runArgs(t, args...); !errors.Is(err, ErrUsage) {
			t.Errorf("%v error = %v, want ErrUsage", args, err)
		}
	}

	if _, err := runArgs(t, "create", "zomboid", "--game", "projectzomboid"); !errors.Is(err, instance.ErrInvalid) {
		t.Errorf("create without an admin password error = %v, want ErrInvalid", err)
	}
}

// covers the safety archive written before a destructive step
func TestArchiveData(t *testing.T) {
	t.Parallel()

	s := store.New(t.TempDir())
	layout := s.Layout("corekeeper")
	inst, err := instance.New("corekeeper", instance.Options{
		MaxPlayers: 5,
		Mode:       instance.ModeNormal,
		InstallDir: layout.Files,
		DataDir:    layout.Data,
	})
	if err != nil {
		t.Fatalf("instance.New() error = %v", err)
	}
	if err := os.MkdirAll(layout.Data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(layout.Data, "ServerConfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := archiveData(s, inst, "pre-update_")
	if err != nil {
		t.Fatalf("archiveData() error = %v", err)
	}
	if res.Path == "" {
		t.Fatal("archiveData() returned an empty path")
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(res.Path), "pre-update_") {
		t.Errorf("archive = %q, want a pre-update name", filepath.Base(res.Path))
	}
	if res.Files != 1 {
		t.Errorf("archive holds %d files, want 1", res.Files)
	}
}

// covers an instance with no data yet, which has nothing to archive
func TestArchiveDataWithoutData(t *testing.T) {
	t.Parallel()

	s := store.New(t.TempDir())
	layout := s.Layout("corekeeper")
	inst, err := instance.New("corekeeper", instance.Options{
		MaxPlayers: 5,
		Mode:       instance.ModeNormal,
		InstallDir: layout.Files,
		DataDir:    layout.Data,
	})
	if err != nil {
		t.Fatalf("instance.New() error = %v", err)
	}
	if err := os.MkdirAll(layout.Data, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := archiveData(s, inst, "pre-update_")
	if err != nil {
		t.Fatalf("archiveData() error = %v", err)
	}
	if res.Path != "" {
		t.Errorf("path = %q, want no archive for an empty data directory", res.Path)
	}
}

// covers update refusing a live server before touching SteamCMD
func TestUpdateRefusesRunningInstance(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	s := store.New(os.Getenv("GAMEHOST_HOME"))
	if err := s.SaveState("corekeeper", instance.State{
		Phase:     instance.PhaseRunning,
		PID:       os.Getpid(),
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := runArgs(t, "update", "corekeeper"); !errors.Is(err, ErrRunning) {
		t.Errorf("update error = %v, want ErrRunning", err)
	}
}

// writeWorld writes a file shaped like a Core Keeper world save.
func writeWorld(t *testing.T, path string) {
	t.Helper()

	// Current saves start with this header; the importer does not parse it.
	content := append([]byte{0x8b, 0x4b, 0x00, 0x00, 0x80, 0xaa, 0xaa, 0xaa}, []byte("world")...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// covers importing an existing save into the instance slot
func TestImportWorld(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	source := filepath.Join(t.TempDir(), "mine.world.gzip")
	writeWorld(t, source)

	out, err := runArgs(t, "import-world", "corekeeper", source)
	if err != nil {
		t.Fatalf("import-world error = %v", err)
	}
	for _, want := range []string{"imported", "slot 0", "next: gamehost start corekeeper"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	root := os.Getenv("GAMEHOST_HOME")
	target := filepath.Join(root, "servers", "corekeeper", "data", "worlds", "0.world.gzip")
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("imported world missing: %v", err)
	}
	want, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("imported world differs from the source")
	}

	backups := filepath.Join(root, "servers", "corekeeper", "backups")
	if entries, err := os.ReadDir(backups); err == nil && len(entries) != 0 {
		t.Errorf("backups = %v, want none for an empty instance", entries)
	}
}

// covers archiving the previous world before an import
func TestImportWorldSavesCurrentData(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	root := os.Getenv("GAMEHOST_HOME")
	data := filepath.Join(root, "servers", "corekeeper", "data")
	if err := os.MkdirAll(filepath.Join(data, "worlds"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "worlds", "0.world.gzip"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "new.world.gzip")
	writeWorld(t, source)

	out, err := runArgs(t, "import-world", "corekeeper", source)
	if err != nil {
		t.Fatalf("import-world error = %v", err)
	}
	if !strings.Contains(out, "saved the current data first") {
		t.Errorf("output = %q, want the safety archive notice", out)
	}

	entries, err := os.ReadDir(filepath.Join(root, "servers", "corekeeper", "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "pre-import_") {
		t.Errorf("backups = %v, want one pre-import archive", entries)
	}
}

// covers rejecting a file that cannot be a Core Keeper world
func TestImportWorldRejectsInvalidFile(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	dir := t.TempDir()

	plain := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plain, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.world.gzip")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{plain, empty} {
		if _, err := runArgs(t, "import-world", "corekeeper", path); !errors.Is(err, ErrUsage) {
			t.Errorf("import-world %q error = %v, want ErrUsage", filepath.Base(path), err)
		}
	}
}

// covers the run lock refusing a second server before it starts
func TestStartRefusesLockedInstance(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	s := store.New(os.Getenv("GAMEHOST_HOME"))
	layout := s.Layout("corekeeper")
	if err := os.WriteFile(filepath.Join(layout.Files, "CoreKeeperServer"), []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}

	// A starting server holds the lock before it publishes its state.
	lock, err := s.Lock("corekeeper")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()

	if _, err := runArgs(t, "start", "corekeeper"); !errors.Is(err, ErrRunning) {
		t.Errorf("start error = %v, want ErrRunning", err)
	}
}

// covers data commands refusing an instance another process holds
func TestCommandsRefuseLockedInstance(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	lock, err := store.New(os.Getenv("GAMEHOST_HOME")).Lock("corekeeper")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()

	source := filepath.Join(t.TempDir(), "mine.world.gzip")
	writeWorld(t, source)

	for _, args := range [][]string{
		{"backup", "corekeeper"},
		{"update", "corekeeper"},
		{"import-world", "corekeeper", source},
	} {
		if _, err := runArgs(t, args...); !errors.Is(err, ErrBusy) {
			t.Errorf("%s error = %v, want ErrBusy", args[0], err)
		}
	}
}

// covers changing instance settings without editing JSON
func TestConfig(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper", "--backup-every", "12h"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	s := store.New(os.Getenv("GAMEHOST_HOME"))
	created, err := s.Load("corekeeper")
	if err != nil {
		t.Fatal(err)
	}
	if created.Backup.Every != "12h" || created.Backup.Keep != instance.DefaultBackupKeep {
		t.Errorf("backup = %+v, want a 12h schedule keeping %d archives", created.Backup, instance.DefaultBackupKeep)
	}

	out, err := runArgs(t, "config", "corekeeper", "--max-players", "3", "--world-name", "Partidita", "--mode", "hard", "--port", "27015", "--backup-every", "24h")
	if err != nil {
		t.Fatalf("config error = %v", err)
	}
	for _, want := range []string{"updated corekeeper", "max players", "3", "Partidita", "world mode", "hard", "direct", "every 24h, keep 5"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}

	updated, err := s.Load("corekeeper")
	if err != nil {
		t.Fatal(err)
	}
	if updated.World.Name != "Partidita" {
		t.Errorf("world name = %q, want Partidita", updated.World.Name)
	}
	if updated.MaxPlayers != 3 || updated.World.Mode != instance.ModeHard || updated.Network.Port != 27015 || updated.Network.Mode != instance.NetworkDirect {
		t.Errorf("updated = %+v, want 3 players, hard mode and a direct port", updated)
	}
	if updated.Backup.Every != "24h" || updated.Backup.Keep != instance.DefaultBackupKeep {
		t.Errorf("backup = %+v, want the new schedule", updated.Backup)
	}

	// Without flags, config prints the settings instead of changing anything.
	out, err = runArgs(t, "config", "corekeeper")
	if err != nil {
		t.Fatalf("config error = %v", err)
	}
	if !strings.Contains(out, "max players") || !strings.Contains(out, "every 24h") {
		t.Errorf("output = %q, want the current settings", out)
	}

	// Turning the schedule off drops its retention with it.
	if _, err := runArgs(t, "config", "corekeeper", "--backup-every", "off"); err != nil {
		t.Fatalf("config off error = %v", err)
	}
	updated, err = s.Load("corekeeper")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Backup.Every != "" || updated.Backup.Keep != 0 {
		t.Errorf("backup = %+v, want a disabled schedule", updated.Backup)
	}

	// An invalid change leaves the stored configuration untouched.
	if _, err := runArgs(t, "config", "corekeeper", "--backup-keep", "3"); err == nil {
		t.Error("config --backup-keep without an interval error = nil, want a failure")
	}
}

// covers config rejecting settings of the other game
func TestConfigRejectsForeignFlags(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "zomboid", "--game", "projectzomboid", "--admin-password", "secret"); err != nil {
		t.Fatalf("create error = %v", err)
	}

	if _, err := runArgs(t, "config", "zomboid", "--max-players", "4"); !errors.Is(err, ErrUsage) {
		t.Errorf("config --max-players error = %v, want ErrUsage", err)
	}
	if _, err := runArgs(t, "config", "zomboid", "--udp-port", "16270"); err != nil {
		t.Errorf("config --udp-port error = %v", err)
	}

	// The name is the ini file and save directory the server already wrote, so
	// renaming it from config would change nothing.
	if _, err := runArgs(t, "config", "zomboid", "--world-name", "renamed"); !errors.Is(err, ErrUsage) {
		t.Errorf("config --world-name error = %v, want ErrUsage", err)
	}
}

// covers config refusing a live server
func TestConfigRefusesRunningInstance(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	s := store.New(os.Getenv("GAMEHOST_HOME"))
	if err := s.SaveState("corekeeper", instance.State{
		Phase: instance.PhaseRunning,
		PID:   os.Getpid(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := runArgs(t, "config", "corekeeper", "--max-players", "2"); !errors.Is(err, ErrRunning) {
		t.Errorf("config error = %v, want ErrRunning", err)
	}
}

// covers refusing an import while the server is alive
func TestImportWorldRefusesRunningInstance(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	s := store.New(os.Getenv("GAMEHOST_HOME"))
	if err := s.SaveState("corekeeper", instance.State{
		Phase: instance.PhaseRunning,
		PID:   os.Getpid(),
	}); err != nil {
		t.Fatal(err)
	}

	source := filepath.Join(t.TempDir(), "mine.world.gzip")
	writeWorld(t, source)
	if _, err := runArgs(t, "import-world", "corekeeper", source); !errors.Is(err, ErrRunning) {
		t.Errorf("import-world error = %v, want ErrRunning", err)
	}
}

// covers the Caves shard and the Klei admin list end to end
func TestDontStarveTogetherCavesAndAdmins(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	out, err := runArgs(t, "create", "cave", "--game", "dontstarvetogether", "--offline", "--caves", "--world-name", "Owls")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if strings.Contains(out, "caves") {
		t.Fatalf("create output = %q", out)
	}
	out, err = runArgs(t, "show", "cave")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out, "caves shard") || !strings.Contains(out, "yes") {
		t.Errorf("show output = %q", out)
	}

	if _, err := runArgs(t, "admin", "add", "cave", "KU_QDNly1sC"); err != nil {
		t.Fatalf("admin add: %v", err)
	}
	out, err = runArgs(t, "admin", "list", "cave")
	if err != nil {
		t.Fatalf("admin list: %v", err)
	}
	if !strings.Contains(out, "KU_QDNly1sC") || !strings.Contains(out, "full") {
		t.Errorf("admin list output = %q", out)
	}

	path := filepath.Join(os.Getenv("GAMEHOST_HOME"), "servers", "cave", "data", "gamehost", "cave", "adminlist.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read adminlist: %v", err)
	}
	if string(data) != "KU_QDNly1sC\n" {
		t.Errorf("adminlist.txt = %q", data)
	}

	// The config verb accepts every setting a cluster really uses, not only the
	// Core Keeper ones.
	out, err = runArgs(t, "config", "cave", "--max-players", "20", "--world-name", "Owls II", "--port", "11009")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	for _, want := range []string{"20", "Owls II", "11009"} {
		if !strings.Contains(out, want) {
			t.Errorf("config output missing %q:\n%s", want, out)
		}
	}

	// A cluster admin list holds Klei ids only, so a name has to be refused
	// instead of silently dropped.
	if _, err := runArgs(t, "admin", "add", "cave", "KU_def12345", "--name", "Wilson"); !errors.Is(err, ErrUsage) {
		t.Errorf("admin add --name error = %v, want ErrUsage", err)
	}

	if _, err := runArgs(t, "admin", "remove", "cave", "KU_QDNly1sC"); err != nil {
		t.Fatalf("admin remove: %v", err)
	}
	out, err = runArgs(t, "admin", "list", "cave")
	if err != nil {
		t.Fatalf("admin list after remove: %v", err)
	}
	if strings.Contains(out, "KU_QDNly1sC") {
		t.Errorf("admin list still shows the removed id: %q", out)
	}

	// The lobby reaches the same list through its own adapter.
	if _, err := lobbyAdminAdd("cave", "KU_abc12345", "", false); err != nil {
		t.Fatalf("lobbyAdminAdd: %v", err)
	}
	list, err := lobbyAdmins("cave")
	if err != nil {
		t.Fatalf("lobbyAdmins: %v", err)
	}
	if len(list) != 1 || list[0].SteamID != "KU_abc12345" || list[0].Privileges != "full" {
		t.Fatalf("lobbyAdmins = %#v", list)
	}
	if _, err := lobbyAdminRemove("cave", "KU_abc12345"); err != nil {
		t.Fatalf("lobbyAdminRemove: %v", err)
	}

	// Project Zomboid has no admin list yet, and that is an error, not a crash.
	if _, err := runArgs(t, "create", "zomboid", "--game", "projectzomboid", "--admin-password", "secret"); err != nil {
		t.Fatalf("create zomboid: %v", err)
	}
	if _, err := runArgs(t, "admin", "list", "zomboid"); err == nil {
		t.Fatal("admin list accepted Project Zomboid")
	}
}

// covers the Project Zomboid mod list end to end
func TestModsCommands(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "zomboid", "--game", "projectzomboid", "--world-name", "base", "--admin-password", "secret"); err != nil {
		t.Fatalf("create: %v", err)
	}

	ini := filepath.Join(os.Getenv("GAMEHOST_HOME"), "servers", "zomboid", "data", "Server", "base.ini")
	if err := os.MkdirAll(filepath.Dir(ini), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "PVP=true\nWorkshopItems=\nMods=\nMaxPlayers=16\n"
	if err := os.WriteFile(ini, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runArgs(t, "mods", "add", "zomboid", "1234567890", "--workshop"); err != nil {
		t.Fatalf("mods add workshop: %v", err)
	}
	if _, err := runArgs(t, "mods", "add", "zomboid", "Hydrocraft"); err != nil {
		t.Fatalf("mods add mod: %v", err)
	}

	out, err := runArgs(t, "mods", "list", "zomboid")
	if err != nil {
		t.Fatalf("mods list: %v", err)
	}
	for _, want := range []string{"1234567890", "Hydrocraft"} {
		if !strings.Contains(out, want) {
			t.Errorf("mods list missing %q:\n%s", want, out)
		}
	}

	data, err := os.ReadFile(ini)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"PVP=true", "WorkshopItems=1234567890", "Mods=Hydrocraft", "MaxPlayers=16"} {
		if !strings.Contains(got, want) {
			t.Errorf("ini lacks %q:\n%s", want, got)
		}
	}

	if _, err := runArgs(t, "mods", "remove", "zomboid", "Hydrocraft"); err != nil {
		t.Fatalf("mods remove: %v", err)
	}
	data, err = os.ReadFile(ini)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Hydrocraft") {
		t.Errorf("mods remove left the id:\n%s", data)
	}

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create corekeeper: %v", err)
	}
	if _, err := runArgs(t, "mods", "list", "corekeeper"); err == nil {
		t.Fatal("mods list accepted Core Keeper")
	}
}

// covers adding, listing and removing admins
func TestAdminCommands(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}

	out, err := runArgs(t, "admin", "add", "corekeeper", "76561198012345678", "--name", "Ramsés")
	if err != nil {
		t.Fatalf("admin add error = %v", err)
	}
	if !strings.Contains(out, "added admin 76561198012345678 (full") {
		t.Errorf("admin add output = %q", out)
	}
	if _, err := runArgs(t, "admin", "add", "corekeeper", "76561198012345679", "--name", "Amigo", "--limited"); err != nil {
		t.Fatalf("admin add error = %v", err)
	}

	out, err = runArgs(t, "admin", "list", "corekeeper")
	if err != nil {
		t.Fatalf("admin list error = %v", err)
	}
	for _, want := range []string{"76561198012345678", "full", "Ramsés", "76561198012345679", "limited", "Amigo"} {
		if !strings.Contains(out, want) {
			t.Errorf("admin list missing %q:\n%s", want, out)
		}
	}

	// The file keeps the shape the server writes.
	data, err := os.ReadFile(filepath.Join(os.Getenv("GAMEHOST_HOME"), "servers", "corekeeper", "data", "Admins.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"index": 1`, `"privileges": 2`, `"steamId": 76561198012345678`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("Admins.json missing %q:\n%s", want, data)
		}
	}

	if out, err := runArgs(t, "admin", "remove", "corekeeper", "76561198012345678"); err != nil {
		t.Fatalf("admin remove error = %v", err)
	} else if !strings.Contains(out, "removed admin") {
		t.Errorf("admin remove output = %q", out)
	}
	out, err = runArgs(t, "admin", "list", "corekeeper")
	if err != nil {
		t.Fatalf("admin list error = %v", err)
	}
	if strings.Contains(out, "76561198012345678") {
		t.Errorf("admin list still shows the removed id:\n%s", out)
	}

	for _, args := range [][]string{
		{"admin"},
		{"admin", "add", "corekeeper", "123"},
		{"admin", "remove", "corekeeper", "76561198099999999"},
		{"admin", "list"},
	} {
		if _, err := runArgs(t, args...); err == nil {
			t.Errorf("%v error = nil, want a failure", args)
		}
	}
}

// covers refusing admin changes while the server runs
func TestAdminRefusesRunningInstance(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	sleeper, err := process.Start(process.Spec{Path: "/bin/sleep", Args: []string{"30"}})
	if err != nil {
		t.Fatalf("process.Start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = process.Stop(sleeper.PID(), time.Second)
		_ = sleeper.WaitContext(context.Background(), time.Second)
	})

	s := store.New(os.Getenv("GAMEHOST_HOME"))
	if err := s.SaveState("corekeeper", instance.State{
		Phase:     instance.PhaseRunning,
		PID:       sleeper.PID(),
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := runArgs(t, "admin", "add", "corekeeper", "76561198012345678"); !errors.Is(err, ErrRunning) {
		t.Errorf("admin add error = %v, want ErrRunning", err)
	}
	if _, err := runArgs(t, "admin", "remove", "corekeeper", "76561198012345678"); !errors.Is(err, ErrRunning) {
		t.Errorf("admin remove error = %v, want ErrRunning", err)
	}
}

// covers the instances serve starts when it boots
func TestServeBootStarts(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	sleeper, err := process.Start(process.Spec{Path: "/bin/sleep", Args: []string{"30"}})
	if err != nil {
		t.Fatalf("process.Start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = process.Stop(sleeper.PID(), time.Second)
		_ = sleeper.WaitContext(context.Background(), time.Second)
	})

	s := store.New(os.Getenv("GAMEHOST_HOME"))
	if err := s.SaveState("corekeeper", instance.State{
		Phase:     instance.PhaseRunning,
		PID:       sleeper.PID(),
		StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// The context is already done: serve runs its boot starts and shuts down.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var stdout, stderr bytes.Buffer
	err = Run(ctx, []string{
		"serve", "--addr", "127.0.0.1:0",
		"--start", "corekeeper",
		"--start", "missing",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("serve error = %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "corekeeper is already running") {
		t.Errorf("output = %q, want the already running notice", out)
	}
	if !strings.Contains(out, `boot start of "missing"`) {
		t.Errorf("output = %q, want the missing instance notice", out)
	}
}

// covers writing the units of an instance with a backup window
func TestServiceInstall(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())
	t.Setenv("DISPLAY", ":0")

	if _, err := runArgs(t, "create", "corekeeper", "--backup-every", "24h", "--backup-keep", "3"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unitDir := t.TempDir()

	out, err := runArgs(t, "service", "install", "corekeeper",
		"--dir", unitDir, "--binary", exe, "--systemctl", "/usr/bin/systemctl")
	if err != nil {
		t.Fatalf("service install error = %v", err)
	}
	if !strings.Contains(out, "wrote "+filepath.Join(unitDir, "gamehost-corekeeper.service")) {
		t.Errorf("output = %q, want the written units", out)
	}
	if !strings.Contains(out, "systemctl --user enable --now gamehost-corekeeper-backup.timer") {
		t.Errorf("output = %q, want the timer next step", out)
	}

	for _, name := range []string{
		"gamehost-corekeeper.service",
		"gamehost-corekeeper-backup.service",
		"gamehost-corekeeper-backup.timer",
	} {
		if _, err := os.Stat(filepath.Join(unitDir, name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}

	mainUnit := readTestFile(t, filepath.Join(unitDir, "gamehost-corekeeper.service"))
	for _, want := range []string{
		"ExecStart=" + exe + " start corekeeper",
		"Restart=on-failure",
		"Environment=GAMEHOST_HOME=" + os.Getenv("GAMEHOST_HOME"),
		"Environment=DISPLAY=:0",
	} {
		if !strings.Contains(mainUnit, want) {
			t.Errorf("main unit missing %q:\n%s", want, mainUnit)
		}
	}
	backup := readTestFile(t, filepath.Join(unitDir, "gamehost-corekeeper-backup.service"))
	if !strings.Contains(backup, "backup corekeeper -keep 3") {
		t.Errorf("backup unit does not keep three archives:\n%s", backup)
	}
	timer := readTestFile(t, filepath.Join(unitDir, "gamehost-corekeeper-backup.timer"))
	if !strings.Contains(timer, "OnUnitActiveSec=1d") {
		t.Errorf("timer does not run daily:\n%s", timer)
	}
}

// covers a unit set without a backup window
func TestServiceInstallWithoutSchedule(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unitDir := t.TempDir()

	if _, err := runArgs(t, "service", "install", "corekeeper", "--dir", unitDir, "--binary", exe); err != nil {
		t.Fatalf("service install error = %v", err)
	}
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "gamehost-corekeeper.service" {
		t.Errorf("units = %v, want only the main service", entries)
	}
}

// covers removing the units and tolerating a missing set
func TestServiceUninstall(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper", "--backup-every", "12h"); err != nil {
		t.Fatalf("create error = %v", err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	unitDir := t.TempDir()
	if _, err := runArgs(t, "service", "install", "corekeeper", "--dir", unitDir, "--binary", exe); err != nil {
		t.Fatalf("service install error = %v", err)
	}

	out, err := runArgs(t, "service", "uninstall", "corekeeper",
		"--dir", unitDir, "--systemctl", "/nonexistent/systemctl")
	if err != nil {
		t.Fatalf("service uninstall error = %v", err)
	}
	if !strings.Contains(out, "removed "+filepath.Join(unitDir, "gamehost-corekeeper.service")) {
		t.Errorf("output = %q, want the removed files", out)
	}
	entries, err := os.ReadDir(unitDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("units left behind: %v", entries)
	}

	// A second uninstall is a no-op, not a failure.
	out, err = runArgs(t, "service", "uninstall", "corekeeper", "--dir", unitDir)
	if err != nil {
		t.Fatalf("second uninstall error = %v", err)
	}
	if !strings.Contains(out, "has no installed units") {
		t.Errorf("output = %q, want the empty notice", out)
	}
}

// covers service usage errors and a missing binary
func TestServiceErrors(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "corekeeper"); err != nil {
		t.Fatalf("create error = %v", err)
	}

	tests := [][]string{
		{"service"},
		{"service", "nope"},
		{"service", "install"},
		{"service", "uninstall"},
		{"service", "install", "corekeeper", "--binary", "/nonexistent/gamehost"},
		{"service", "install", "missing"},
	}
	for _, args := range tests {
		if _, err := runArgs(t, args...); err == nil {
			t.Errorf("%v error = nil, want a failure", args)
		}
	}
}

// readTestFile returns the content of one test file.
func readTestFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// covers keeping the log complete after the terminal goes away
func TestServerOutputKeepsTheLogAfterAHangup(t *testing.T) {
	var log bytes.Buffer
	out := serverOutput(hungUpWriter{}, &log)

	line := "corekeeper is stopped after 1h23m54s (exit 0)\n"
	if _, err := out.Write([]byte(line)); err != nil {
		t.Fatalf("write error = %v, want a dead terminal to be ignored", err)
	}
	if got := log.String(); got != line {
		t.Fatalf("log = %q, want %q", got, line)
	}
}

// hungUpWriter is a terminal whose window was closed.
type hungUpWriter struct{}

func (hungUpWriter) Write([]byte) (int, error) { return 0, errors.New("input/output error") }

// covers the heap of the project zomboid server
func TestProjectZomboidMemory(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "zomboid", "--game", "projectzomboid",
		"--admin-password", "secret", "--memory", "4G"); err != nil {
		t.Fatalf("create error = %v", err)
	}

	s := store.New(os.Getenv("GAMEHOST_HOME"))
	inst, err := s.Load("zomboid")
	if err != nil {
		t.Fatal(err)
	}
	if inst.Memory != "4g" {
		t.Errorf("Memory = %q, want the value normalized to 4g", inst.Memory)
	}

	out, err := runArgs(t, "show", "zomboid")
	if err != nil {
		t.Fatalf("show error = %v", err)
	}
	if !strings.Contains(out, "memory") || !strings.Contains(out, "4g") {
		t.Errorf("show output = %q, want the heap", out)
	}

	out, err = runArgs(t, "config", "zomboid", "--memory", "2g")
	if err != nil {
		t.Fatalf("config error = %v", err)
	}
	if !strings.Contains(out, "updated zomboid") || !strings.Contains(out, "2g") {
		t.Errorf("config output = %q", out)
	}
	if inst, err = s.Load("zomboid"); err != nil || inst.Memory != "2g" {
		t.Errorf("stored memory = %q (error %v), want 2g", inst.Memory, err)
	}

	// An empty value restores the default the server ships with.
	if _, err := runArgs(t, "config", "zomboid", "--memory", ""); err != nil {
		t.Fatalf("config --memory \"\" error = %v", err)
	}
	if inst, err = s.Load("zomboid"); err != nil || inst.Memory != "" {
		t.Errorf("stored memory = %q (error %v), want the default", inst.Memory, err)
	}
	out, err = runArgs(t, "config", "zomboid")
	if err != nil {
		t.Fatalf("config error = %v", err)
	}
	if !strings.Contains(out, "8g (default)") {
		t.Errorf("config output = %q, want the default heap", out)
	}

	// Below 512m nothing boots, so the value is refused and nothing changes.
	if _, err := runArgs(t, "config", "zomboid", "--memory", "256m"); !errors.Is(err, instance.ErrInvalid) {
		t.Errorf("config --memory 256m error = %v, want ErrInvalid", err)
	}
	if inst, err = s.Load("zomboid"); err != nil || inst.Memory != "" {
		t.Errorf("a refused heap changed the instance: %q", inst.Memory)
	}

	// The flag belongs to project zomboid alone.
	if _, err := runArgs(t, "create", "corekeeper", "--memory", "4g"); !errors.Is(err, ErrUsage) {
		t.Errorf("create corekeeper --memory error = %v, want ErrUsage", err)
	}
	if _, err := runArgs(t, "create", "keeper"); err != nil {
		t.Fatalf("create corekeeper error = %v", err)
	}
	if _, err := runArgs(t, "config", "keeper", "--memory", "4g"); !errors.Is(err, ErrUsage) {
		t.Errorf("config corekeeper --memory error = %v, want ErrUsage", err)
	}
}
