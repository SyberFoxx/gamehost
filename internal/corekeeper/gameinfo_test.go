package corekeeper

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGameInfo creates a GameInfo.txt in a temporary install directory.
func writeGameInfo(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, GameInfoFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// covers parsing the id out of the surrounding text
func TestReadGameID(t *testing.T) {
	t.Parallel()

	dir := writeGameInfo(t, "No GameID found. Creating new GameID for the session.\n\nGameID: u7iGUkFwJL65k5D")

	got, err := ReadGameID(dir)
	if err != nil {
		t.Fatalf("ReadGameID() error = %v", err)
	}
	if got != "u7iGUkFwJL65k5D" {
		t.Errorf("ReadGameID() = %q, want %q", got, "u7iGUkFwJL65k5D")
	}
}

// covers a missing file and a file without the line
func TestReadGameIDErrors(t *testing.T) {
	t.Parallel()

	if _, err := ReadGameID(t.TempDir()); err == nil {
		t.Error("ReadGameID() error = nil, want a failure for a missing file")
	}

	dir := writeGameInfo(t, "nothing useful here\n")
	if _, err := ReadGameID(dir); err == nil {
		t.Error("ReadGameID() error = nil, want a failure for a missing line")
	}
}
