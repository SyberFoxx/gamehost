package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// covers the gate that decides between the lobby and the usage text
func TestWantsLobby(t *testing.T) {
	t.Parallel()

	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() { _ = stdin.Close() }()

	var buffer bytes.Buffer
	tests := []struct {
		name   string
		args   []string
		stdout *bytes.Buffer
		stdin  *os.File
		want   bool
	}{
		{"a command never opens the lobby", []string{"list"}, &buffer, stdin, false},
		{"a buffer is not a terminal", nil, &buffer, stdin, false},
		{"no stdin never opens the lobby", nil, &buffer, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := wantsLobby(tt.args, tt.stdout, tt.stdin); got != tt.want {
				t.Fatalf("wantsLobby = %v, want %v", got, tt.want)
			}
		})
	}
}

// covers the adapter that turns instances into lobby rows
func TestLobbyItems(t *testing.T) {
	// t.Setenv forbids t.Parallel.
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "partidita", "--max-players", "4"); err != nil {
		t.Fatalf("create: %v", err)
	}

	items, err := lobbyItems()
	if err != nil {
		t.Fatalf("lobbyItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}

	item := items[0]
	if item.Name != "partidita" {
		t.Errorf("name = %q, want partidita", item.Name)
	}
	if item.Game != "Core Keeper" {
		t.Errorf("game = %q, want the display label", item.Game)
	}
	if item.Phase != "stopped" {
		t.Errorf("phase = %q, want stopped", item.Phase)
	}
	if item.PID != 0 {
		t.Errorf("pid = %d, want 0", item.PID)
	}
	if item.JoinLabel != "game id" || item.JoinValue == "" {
		t.Errorf("join = %q %q, want the game id of the instance", item.JoinLabel, item.JoinValue)
	}
}

// covers reading the newest log through the lobby adapter
func TestLobbyLogs(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "partidita"); err != nil {
		t.Fatalf("create: %v", err)
	}

	s, err := openStore()
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	dir := s.Layout("partidita").Logs
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "2026-11-05_10-00-00.log")
	if err := os.WriteFile(path, []byte("boot\nworld loaded\nready\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	lines, err := lobbyLogs("partidita", 100)
	if err != nil {
		t.Fatalf("lobbyLogs: %v", err)
	}
	want := []string{"boot", "world loaded", "ready"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %q", len(lines), len(want), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

// covers the empty-log path the lobby shows as a hint
func TestLobbyLogsWithoutLogFiles(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "partidita"); err != nil {
		t.Fatalf("create: %v", err)
	}

	lines, err := lobbyLogs("partidita", 100)
	if err != nil {
		t.Fatalf("lobbyLogs: %v", err)
	}
	if lines != nil {
		t.Fatalf("got %q, want no lines", lines)
	}
}

// covers the two helpers that pick the useful line of a report
func TestReportLineHelpers(t *testing.T) {
	t.Parallel()

	report := "stopping partidita (pid 12), waiting up to 20s\n\npartidita stopped\n"
	if got := firstLine(report); got != "stopping partidita (pid 12), waiting up to 20s" {
		t.Errorf("firstLine = %q", got)
	}
	if got := lastLine(report); got != "partidita stopped" {
		t.Errorf("lastLine = %q", got)
	}
	if got := firstLine(""); got != "" {
		t.Errorf("firstLine of empty text = %q, want empty", got)
	}
}

// covers the empty store the lobby shows as "no instances yet"
func TestLobbyItemsWithoutInstances(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	items, err := lobbyItems()
	if err != nil {
		t.Fatalf("lobbyItems: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("got %d items, want none", len(items))
	}
}
