package corekeeper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// covers the launch command and the session id lookup
func TestRunner(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	inst, err := instance.New("corekeeper", instance.Options{
		MaxPlayers: 4,
		Mode:       instance.ModeNormal,
		InstallDir: dir,
		DataDir:    filepath.Join(dir, "data"),
	})
	if err != nil {
		t.Fatalf("instance.New() error = %v", err)
	}

	runner := Runner{}
	launch, err := runner.Launch(inst)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if want := BinaryPath(dir); launch.Path != want {
		t.Errorf("Launch().Path = %q, want %q", launch.Path, want)
	}
	if launch.Dir != dir {
		t.Errorf("Launch().Dir = %q, want the install dir", launch.Dir)
	}
	if len(launch.Args) == 0 {
		t.Error("Launch().Args is empty")
	}

	if label, value := runner.Join(inst); label != "game id" || value != "" {
		t.Errorf("Join() = %q, %q; want an empty game id without GameInfo.txt", label, value)
	}

	info := "No GameID found. Creating new GameID for the session.\n\nGameID: u7iGUkFwJL65k5D\n"
	if err := os.WriteFile(filepath.Join(dir, "GameInfo.txt"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
	if id := runner.SessionID(inst); id != "u7iGUkFwJL65k5D" {
		t.Errorf("SessionID() = %q, want the written id", id)
	}
}
