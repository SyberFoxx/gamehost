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
	_ = server.WaitContext(context.Background(), time.Second)
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

// covers the safety archive written before an update
func TestBackupBeforeUpdate(t *testing.T) {
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

	var out bytes.Buffer
	path, err := backupBeforeUpdate(s, inst, &out)
	if err != nil {
		t.Fatalf("backupBeforeUpdate() error = %v", err)
	}
	if path == "" {
		t.Fatal("backupBeforeUpdate() returned an empty path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(path), "pre-update_") {
		t.Errorf("archive = %q, want a pre-update name", filepath.Base(path))
	}
	if !strings.Contains(out.String(), "backed up corekeeper before the update") {
		t.Errorf("output = %q, want the backup notice", out.String())
	}
}

// covers an instance that was never started, so it has nothing to archive
func TestBackupBeforeUpdateWithoutData(t *testing.T) {
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

	var out bytes.Buffer
	path, err := backupBeforeUpdate(s, inst, &out)
	if err != nil {
		t.Fatalf("backupBeforeUpdate() error = %v", err)
	}
	if path != "" || out.Len() != 0 {
		t.Errorf("path = %q, output = %q; want no backup without data", path, out.String())
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
