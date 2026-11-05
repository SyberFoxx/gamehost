package tui

import (
	"strings"
	"testing"
	"time"

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
	if width > 80 {
		t.Fatalf("the wordmark is %d columns wide, want at most 80", width)
	}
	for i, row := range rows {
		if got := lipgloss.Width(row); got != width {
			t.Fatalf("row %d is %d columns wide, want %d", i, got, width)
		}
	}
}

func TestLobbyShowsBannerMenuAndInstance(t *testing.T) {
	t.Parallel()
	m := newLobby(t, &recorder{})
	m.items = []Item{{
		Name:      "Partidita",
		Game:      "Core Keeper",
		Phase:     "running",
		PID:       733874,
		StartedAt: time.Now().Add(-2 * time.Hour),
		JoinLabel: "game",
		JoinValue: "K85KQswu7GWLsQB",
	}}

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
			m := newLobby(t, &recorder{})
			m.width, m.height = size.width, size.height
			m.items = []Item{{Name: "Partidita", Phase: "stopped"}}

			content := m.View().Content
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
	m := newLobby(t, &recorder{})

	content := m.View().Content
	if !strings.Contains(content, "gamehost create my-world") {
		t.Fatalf("the empty lobby is missing the create hint:\n%s", content)
	}
}

func TestFooterShowsProgressAndErrors(t *testing.T) {
	t.Parallel()
	m := newLobby(t, &recorder{})
	m.busy, m.note = true, "Start Partidita…"

	content := m.View().Content
	if !strings.Contains(content, "Start Partidita") {
		t.Fatalf("the footer is missing the running action:\n%s", content)
	}

	m.busy, m.bad, m.note = false, true, "instance is running, stop it first"
	content = m.View().Content
	if !strings.Contains(content, "stop it first") {
		t.Fatalf("the footer is missing the error:\n%s", content)
	}
}

func TestLogsViewScrollsToTheNewestLines(t *testing.T) {
	t.Parallel()
	m := newLobby(t, &recorder{})
	m.items = []Item{{Name: "Partidita"}}
	m.screen = screenLogs
	m.height = 20
	m.logs = []string{"boot", "loading world", "ready"}

	content := m.View().Content
	for _, want := range []string{"Logs · Partidita", "boot", "ready"} {
		if !strings.Contains(content, want) {
			t.Fatalf("the log screen is missing %q:\n%s", want, content)
		}
	}
}

func TestLogsViewWithoutLogsSaysWhatToDo(t *testing.T) {
	t.Parallel()
	m := newLobby(t, &recorder{})
	m.items = []Item{{Name: "Partidita"}}
	m.screen = screenLogs

	if !strings.Contains(m.View().Content, "start the server first") {
		t.Fatalf("an empty log screen should explain what to do")
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
}
