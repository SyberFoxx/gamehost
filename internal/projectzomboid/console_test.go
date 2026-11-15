package projectzomboid

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestConsolePath keeps the control file inside the instance data directory,
// where `stop` can find it from instance.json alone.
func TestConsolePath(t *testing.T) {
	t.Parallel()

	inst := modInstance(t, "PVP=true\n")
	want := filepath.Join(inst.DataDir, ConsoleFile)
	if got := (Runner{}).ConsolePath(inst); got != want {
		t.Fatalf("ConsolePath() = %q, want %q", got, want)
	}
	if filepath.Dir(want) != inst.DataDir {
		t.Fatalf("console file %q is not in %q", want, inst.DataDir)
	}
}

// TestStopCommands covers the shutdown the PZ devs document: save first, quit
// second, never a signal.
func TestStopCommands(t *testing.T) {
	t.Parallel()

	inst := modInstance(t, "PVP=true\n")
	got := strings.Join((Runner{}).StopCommands(inst), ",")
	if got != "save,quit" {
		t.Fatalf("StopCommands() = %q, want %q", got, "save,quit")
	}
}
