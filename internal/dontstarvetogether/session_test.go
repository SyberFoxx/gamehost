package dontstarvetogether

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// writeMasterLog writes the Master shard log of an instance.
func writeMasterLog(t *testing.T, inst instance.Instance, data string) {
	t.Helper()
	path := ServerLogPath(inst)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create log dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
}

func TestSessionIDReadsLatestLogLine(t *testing.T) {
	inst := testInstance(t)
	writeMasterLog(t, inst, ""+
		"[00:00:01] starting up\n"+
		"Telling Client our new session identifier: D8835938996F494F\n"+
		"[00:00:02] players connected\n"+
		"Telling Client our new session identifier: E55C3FFDD251D8A7\n")

	if got := (Runner{}).SessionID(inst); got != "E55C3FFDD251D8A7" {
		t.Errorf("SessionID = %q, want the latest identifier", got)
	}
}

func TestSessionIDWithoutLog(t *testing.T) {
	inst := testInstance(t)
	if got := (Runner{}).SessionID(inst); got != "" {
		t.Errorf("SessionID = %q, want empty before the first start", got)
	}
}

func TestSessionIDIgnoresUnrelatedLines(t *testing.T) {
	inst := testInstance(t)
	writeMasterLog(t, inst, ""+
		"session identifier: not the line we want\n"+
		"Telling Client our new Session Identifier: ABCDEF0123456789\n"+
		"Telling Client our new session identifier:\n")

	if got := (Runner{}).SessionID(inst); got != "" {
		t.Errorf("SessionID = %q, want empty when no exact line exists", got)
	}
}

func TestJoinUsesSessionID(t *testing.T) {
	inst := testInstance(t)
	writeMasterLog(t, inst, "Telling Client our new session identifier: D8835938996F494F\n")
	label, value := (Runner{}).Join(inst)
	if label != "session id" || value != "D8835938996F494F" {
		t.Errorf("Join = %q %q, want session id D8835938996F494F", label, value)
	}
}

func TestJoinFallsBackToClusterBeforeFirstStart(t *testing.T) {
	inst := testInstance(t)
	label, value := (Runner{}).Join(inst)
	if label != "cluster" || value != inst.World.Name {
		t.Errorf("Join = %q %q, want cluster %q", label, value, inst.World.Name)
	}
}
