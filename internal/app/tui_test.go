package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/tui"
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
func TestLobbyFollow(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := lobbyFollow(ctx, "partidita", 100)

	want := []string{"boot", "world loaded", "ready"}
	var lines []string
	for len(lines) < len(want) {
		select {
		case event, ok := <-events:
			if !ok {
				t.Fatalf("the stream closed after %q", lines)
			}
			if event.Err != nil {
				t.Fatalf("follow: %v", event.Err)
			}
			if event.Line != "" {
				lines = append(lines, event.Line)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out after %q", lines)
		}
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

// covers the empty-log path the lobby shows as a hint
func TestLobbyFollowWithoutLogFiles(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t, "create", "partidita"); err != nil {
		t.Fatalf("create: %v", err)
	}

	events := lobbyFollow(context.Background(), "partidita", 100)

	var lines []string
	for event := range events {
		if event.Err != nil {
			t.Fatalf("follow: %v", event.Err)
		}
		if event.Line != "" {
			lines = append(lines, event.Line)
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "start it first") {
		t.Fatalf("got %q, want one hint line", lines)
	}
}

// covers the settings round trip the lobby uses to read and save a form
func TestSettingsRoundTrip(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	if _, err := runArgs(t,
		"create", "partidita",
		"--max-players", "4",
		"--world-name", "Party",
		"--mode", "hard",
		"--password", "secret",
		"--backup-every", "24h",
		"--backup-keep", "3",
	); err != nil {
		t.Fatalf("create: %v", err)
	}

	s, inst, err := loadInstance("partidita")
	if err != nil {
		t.Fatalf("loadInstance: %v", err)
	}

	settings := settingsOf(inst)
	if settings.MaxPlayers != "4" || settings.WorldName != "Party" || settings.Mode != "hard" {
		t.Fatalf("settings = %#v", settings)
	}
	if settings.Password != "secret" || settings.BackupEvery != "24h" || settings.BackupKeep != "3" {
		t.Fatalf("settings = %#v", settings)
	}

	opts, err := updateOptions(inst, settings)
	if err != nil {
		t.Fatalf("updateOptions: %v", err)
	}
	if opts.InstallDir != s.Layout(inst.Name).Files || opts.DataDir != s.Layout(inst.Name).Data {
		t.Fatalf("updateOptions lost the directories: %#v", opts)
	}

	updated, err := instance.New(inst.Name, opts)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if updated.MaxPlayers != 4 || updated.World.Name != "Party" || updated.World.Mode != instance.ModeHard {
		t.Fatalf("rebuilt instance = %#v", updated)
	}
}

// covers the validation the two option parsers share
func TestCreateOptionsRejectsBadValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings tui.Settings
	}{
		{"unknown game", tui.Settings{Game: "doom"}},
		{"bad mode", tui.Settings{Game: instance.GameCoreKeeper, Mode: "insane"}},
		{"bad players", tui.Settings{Game: instance.GameCoreKeeper, MaxPlayers: "many"}},
		{"bad port", tui.Settings{Game: instance.GameProjectZomboid, Port: "http"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := createOptions(tt.settings); err == nil {
				t.Fatalf("createOptions accepted %#v", tt.settings)
			}
		})
	}
}

// covers the cluster settings the wizard sends back for Don't Starve Together
func TestDontStarveTogetherOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   tui.Settings
		want    instance.Network
		wantErr bool
	}{
		{
			name:  "online keeps the token",
			input: tui.Settings{Game: instance.GameDontStarveTogether, MaxPlayers: "6", Network: "online", ClusterToken: " pds-g^KU_test "},
			want:  instance.Network{Mode: instance.NetworkOnline, Port: instance.DontStarveTogetherPort},
		},
		{
			name:  "offline ignores the token",
			input: tui.Settings{Game: instance.GameDontStarveTogether, MaxPlayers: "6", Network: "offline"},
			want:  instance.Network{Mode: instance.NetworkOffline, Port: instance.DontStarveTogetherPort},
		},
		{name: "online without a token", input: tui.Settings{Game: instance.GameDontStarveTogether, MaxPlayers: "6", Network: "online"}, wantErr: true},
		{name: "too many players", input: tui.Settings{Game: instance.GameDontStarveTogether, MaxPlayers: "65", Network: "online", ClusterToken: "t"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := createOptions(tt.input)
			if tt.wantErr {
				if err == nil {
					// The token is checked by the model, not by the parser.
					if _, err := instance.New("cave", opts); err == nil {
						t.Fatalf("createOptions accepted %#v", tt.input)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("createOptions: %v", err)
			}

			opts.InstallDir = "/srv/files"
			opts.DataDir = "/srv/data"
			inst, err := instance.New("cave", opts)
			if err != nil {
				t.Fatalf("new instance: %v", err)
			}
			if inst.Network != tt.want {
				t.Errorf("Network = %+v, want %+v", inst.Network, tt.want)
			}
		})
	}
}

// covers the empty values a form sends back for a Core Keeper instance
func TestCreateOptionsDefaults(t *testing.T) {
	t.Parallel()

	opts, err := createOptions(tui.Settings{Game: instance.GameCoreKeeper})
	if err != nil {
		t.Fatalf("createOptions: %v", err)
	}
	if opts.Mode != instance.ModeNormal {
		t.Fatalf("mode = %q, want normal", opts.Mode)
	}
	if opts.BackupEvery != "" {
		t.Fatalf("backup every = %q, want off", opts.BackupEvery)
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
