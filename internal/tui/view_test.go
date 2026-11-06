package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestWordmarkIsSixEvenRows(t *testing.T) {
	t.Parallel()
	rows := strings.Split(wordmark(), "\n")
	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6", len(rows))
	}

	width := lipgloss.Width(rows[0])
	if width == 0 {
		t.Fatalf("the wordmark is empty")
	}
	if width > 90 {
		t.Fatalf("the wordmark is %d columns wide, want at most 90", width)
	}
	for i, row := range rows {
		if got := lipgloss.Width(row); got != width {
			t.Fatalf("row %d is %d columns wide, want %d", i, got, width)
		}
	}
}

// lobbyModel returns a root model showing the lobby with the given items.
func lobbyModel(t *testing.T, items []Item) Model {
	t.Helper()
	f := &fake{items: items}
	m := newTestModel(t, f, nil)
	m, _ = step(t, m, listMsg{items: items})
	return m
}

func TestLobbyShowsBannerMenuAndInstance(t *testing.T) {
	t.Parallel()
	m := lobbyModel(t, []Item{{
		Name:      "Partidita",
		Game:      "Core Keeper",
		Phase:     "running",
		PID:       733874,
		StartedAt: time.Now().Add(-2 * time.Hour),
		JoinLabel: "game id",
		JoinValue: "K85KQswu7GWLsQB",
	}})

	view := m.View()
	if !view.AltScreen {
		t.Fatalf("the lobby must own the alternate screen")
	}
	for _, want := range []string{"█", "› Start", "Quit", "Partidita", "Core Keeper", "pid 733874", "K85KQswu7GWLsQB"} {
		if !strings.Contains(view.Content, want) {
			t.Fatalf("the lobby is missing %q:\n%s", want, view.Content)
		}
	}
}

func TestNarrowOrShortTerminalUsesTheName(t *testing.T) {
	t.Parallel()
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{"narrow", 60, 40},
		{"short", 100, 20},
	} {
		t.Run(size.name, func(t *testing.T) {
			m := lobbyModel(t, []Item{{Name: "Partidita", Phase: "stopped"}})
			m.width, m.height = size.width, size.height

			content := m.render()
			if !strings.Contains(content, "GAMEHOST") {
				t.Fatalf("the short banner is missing:\n%s", content)
			}
			if strings.Contains(content, "█") {
				t.Fatalf("a small terminal should not draw the block banner:\n%s", content)
			}
		})
	}
}

func TestEmptyLobbyExplainsHowToCreateAnInstance(t *testing.T) {
	t.Parallel()
	m := lobbyModel(t, nil)

	content := m.render()
	for _, want := range []string{"No instances yet", "New instance"} {
		if !strings.Contains(content, want) {
			t.Fatalf("the empty lobby is missing %q:\n%s", want, content)
		}
	}
}

func TestFooterShowsProgressAndErrors(t *testing.T) {
	t.Parallel()
	m := lobbyModel(t, []Item{{Name: "Partidita"}})
	m.note = "starting Partidita…"

	content := m.render()
	if !strings.Contains(content, "starting Partidita") {
		t.Fatalf("the footer is missing the running action:\n%s", content)
	}

	m.bad, m.note = true, "instance is running, stop it first"
	content = m.render()
	if !strings.Contains(content, "stop it first") {
		t.Fatalf("the footer is missing the error:\n%s", content)
	}
}

func TestLogsViewScrollsToTheNewestLines(t *testing.T) {
	t.Parallel()
	f := &fake{follow: make(chan Event)}
	logs := newLogs(context.Background(), f.backend().Logs, "Partidita").(*logs)
	logs.lines = []string{"boot", "loading world", "ready"}

	content := logs.View(100, 20)
	for _, want := range []string{"Logs · Partidita", "boot", "ready"} {
		if !strings.Contains(content, want) {
			t.Fatalf("the log screen is missing %q:\n%s", want, content)
		}
	}
}

func TestLogsViewWithoutLogsSaysWhatToDo(t *testing.T) {
	t.Parallel()
	f := &fake{follow: make(chan Event)}
	logs := newLogs(context.Background(), f.backend().Logs, "Partidita").(*logs)
	logs.done = true

	if !strings.Contains(logs.View(100, 20), "start the server first") {
		t.Fatalf("an empty log screen should explain what to do")
	}
}

func TestPanelBoxIsClosed(t *testing.T) {
	t.Parallel()
	m := lobbyModel(t, []Item{{
		Name:      "Partidita",
		Game:      "Core Keeper",
		Phase:     "running",
		PID:       733874,
		StartedAt: time.Now().Add(-time.Hour),
		JoinLabel: "game id",
		JoinValue: "K85KQswu7GWLsQB",
	}})

	content := m.render()
	var box []string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, "╭") {
			box = append(box, line)
			continue
		}
		if len(box) > 0 {
			box = append(box, line)
			if strings.Contains(line, "╰") {
				break
			}
		}
	}
	if len(box) < 3 {
		t.Fatalf("the instance panel is missing:\n%s", content)
	}

	want := lipgloss.Width(box[0])
	for i, line := range box {
		if got := lipgloss.Width(line); got != want {
			t.Fatalf("panel line %d is %d columns wide, want %d: %q", i, got, want, line)
		}
	}
}

func TestHelpOverlayListsBackedCommandLines(t *testing.T) {
	t.Parallel()
	m := lobbyModel(t, []Item{{Name: "Partidita"}})
	m, _ = step(t, m, press("?"))

	content := m.render()
	for _, want := range []string{"Keys", "gamehost list", "gamehost create", "ctrl+c"} {
		if !strings.Contains(content, want) {
			t.Fatalf("the help overlay is missing %q:\n%s", want, content)
		}
	}
}

func TestConfirmOverlayReplacesTheScreen(t *testing.T) {
	t.Parallel()
	m := lobbyModel(t, []Item{{Name: "Partidita"}})
	m, _ = step(t, m, confirmMsg{
		question: "Stop Partidita?",
		detail:   "The server saves the world and exits.",
		yes:      func() tea.Msg { return statusMsg{text: "stopped"} },
	})

	content := m.render()
	if !strings.Contains(content, "Stop Partidita?") || !strings.Contains(content, "y") {
		t.Fatalf("the confirmation is missing:\n%s", content)
	}
	if strings.Contains(content, "› Start") {
		t.Fatalf("the confirmation should replace the screen:\n%s", content)
	}
}

func TestFormatUptime(t *testing.T) {
	t.Parallel()
	tests := []struct {
		elapsed time.Duration
		want    string
	}{
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m"},
		{14 * time.Minute, "14m"},
		{2*time.Hour + 14*time.Minute, "2h14m"},
		{time.Hour, "1h"},
		{3*time.Hour + 5*time.Minute + 30*time.Second, "3h05m"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := formatUptime(tt.elapsed); got != tt.want {
				t.Fatalf("formatUptime(%s) = %q, want %q", tt.elapsed, got, tt.want)
			}
		})
	}
}

func TestHumanBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		bytes int64
		want  string
	}{
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{3 * 1024 * 1024 * 1024, "3.0 GiB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := humanBytes(tt.bytes); got != tt.want {
				t.Fatalf("humanBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestClipKeepsTheWidth(t *testing.T) {
	t.Parallel()
	if got := clip("short", 10); got != "short" {
		t.Fatalf("clip changed a short line: %q", got)
	}
	got := clip("0123456789", 5)
	if lipgloss.Width(got) != 5 {
		t.Fatalf("clip returned %q, want 5 columns", got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("clip did not mark the cut: %q", got)
	}
	if got := clip("anything", 0); got != "anything" {
		t.Fatalf("clip with no width should not cut: %q", got)
	}
}

func TestTailWindow(t *testing.T) {
	t.Parallel()
	lines := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

	tests := []struct {
		name   string
		rows   int
		scroll int
		want   []string
	}{
		{"live edge", 3, 0, []string{"7", "8", "9"}},
		{"scrolled back", 3, 2, []string{"5", "6", "7"}},
		{"everything", 0, 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tailWindow(lines, tt.rows, tt.scroll)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("tailWindow = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAppendLinesKeepsEmbeddedNewlines(t *testing.T) {
	t.Parallel()
	lines := appendLines(nil, "first\nsecond")
	lines = appendLines(lines, "third\n")
	lines = appendLines(lines, "")

	want := "first,second,third"
	if got := strings.Join(lines, ","); got != want {
		t.Fatalf("appendLines = %q, want %q", got, want)
	}
}

func TestStatusStyleMarksEveryVerdict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status string
		marker string
	}{
		{"ok", "✓"},
		{"active", "✓"},
		{"warn", "!"},
		{"limited", "!"},
		{"fail", "✗"},
		{"", "✗"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			marker, _ := statusStyle(tt.status)
			if marker != tt.marker {
				t.Fatalf("statusStyle(%q) = %q, want %q", tt.status, marker, tt.marker)
			}
		})
	}
}
