package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// recorder is a Backend that remembers what the lobby asked of it.
type recorder struct {
	items   []Item
	listErr error
	logs    []string
	logErr  error

	called string
	name   string
	text   string
	runErr error
}

func (r *recorder) backend() Backend {
	return Backend{
		List:   func() ([]Item, error) { return r.items, r.listErr },
		Start:  func(_ context.Context, name string) (string, error) { return r.record("start", name) },
		Stop:   func(_ context.Context, name string) (string, error) { return r.record("stop", name) },
		Backup: func(_ context.Context, name string) (string, error) { return r.record("backup", name) },
		Logs: func(name string, _ int) ([]string, error) {
			r.called, r.name = "logs", name
			return r.logs, r.logErr
		},
	}
}

func (r *recorder) record(action, name string) (string, error) {
	r.called, r.name = action, name
	return r.text, r.runErr
}

// step applies one message and returns the model plus the command it asked for.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want tui.Model", next)
	}
	return model, cmd
}

// drain runs a command and folds a batch into the messages it produced.
func drain(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, inner := range batch {
			out = append(out, drain(t, inner)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// message returns the first message of the requested type.
func message[T tea.Msg](t *testing.T, msgs []tea.Msg) T {
	t.Helper()
	for _, msg := range msgs {
		if found, ok := msg.(T); ok {
			return found
		}
	}
	var zero T
	t.Fatalf("no %T in %v", zero, msgs)
	return zero
}

func newLobby(t *testing.T, r *recorder) Model {
	t.Helper()
	m := newModel(context.Background(), r.backend())
	m.width, m.height = 100, 40
	return m
}

func TestInitLoadsInstances(t *testing.T) {
	t.Parallel()
	r := &recorder{items: []Item{{Name: "Partidita"}, {Name: "Otro"}}}
	m := newLobby(t, r)

	msgs := drain(t, m.Init())
	loaded := message[listMsg](t, msgs)
	if loaded.err != nil {
		t.Fatalf("list: %v", loaded.err)
	}

	m, _ = step(t, m, loaded)
	if len(m.items) != 2 {
		t.Fatalf("got %d items, want 2", len(m.items))
	}
}

func TestMenuCursorMovesAndClamps(t *testing.T) {
	t.Parallel()
	m := newLobby(t, &recorder{})

	steps := []struct {
		name string
		key  tea.KeyPressMsg
		want int
	}{
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, 1},
		{"down again", tea.KeyPressMsg{Code: tea.KeyDown}, 2},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, 1},
		{"letter j", tea.KeyPressMsg{Code: 'j', Text: "j"}, 2},
	}
	for _, tc := range steps {
		t.Run(tc.name, func(t *testing.T) {
			var cmd tea.Cmd
			m, cmd = step(t, m, tc.key)
			if cmd != nil {
				t.Fatalf("moving the cursor returned a command")
			}
			if m.cursor != tc.want {
				t.Fatalf("cursor = %d, want %d", m.cursor, tc.want)
			}
		})
	}

	// The list never wraps: the top and bottom clamp.
	for range menu {
		m, _ = step(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.cursor != len(menu)-1 {
		t.Fatalf("cursor = %d, want %d", m.cursor, len(menu)-1)
	}
	for range menu {
		m, _ = step(t, m, tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", m.cursor)
	}
}

func TestQuitKeysLeaveTheProgram(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"q", "ctrl+c"} {
		t.Run(key, func(t *testing.T) {
			m := newLobby(t, &recorder{})
			_, cmd := m.key(key)
			if _, ok := drain(t, cmd)[0].(tea.QuitMsg); !ok {
				t.Fatalf("%s did not ask to quit", key)
			}
		})
	}
}

func TestActivateWithoutInstancesExplainsWhatToDo(t *testing.T) {
	t.Parallel()
	m := newLobby(t, &recorder{})

	m, cmd := step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("activating an empty lobby returned a command")
	}
	if !strings.Contains(m.note, "gamehost create") {
		t.Fatalf("note = %q, want a create hint", m.note)
	}
	if !m.bad {
		t.Fatalf("an empty lobby should report the problem as an error")
	}
}

func TestStartRunsTheActionAwayFromTheUI(t *testing.T) {
	t.Parallel()
	r := &recorder{items: []Item{{Name: "Partidita"}}, text: "Partidita is running (pid 42)"}
	m := newLobby(t, r)
	m, _ = step(t, m, listMsg{items: r.items})

	m, cmd := step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.busy {
		t.Fatalf("the lobby should be busy while the action runs")
	}
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want the Start entry", m.cursor)
	}

	result := message[resultMsg](t, drain(t, cmd))
	if result.err != nil {
		t.Fatalf("start: %v", result.err)
	}
	if r.called != "start" || r.name != "Partidita" {
		t.Fatalf("backend called %q on %q, want start on Partidita", r.called, r.name)
	}

	m, cmd = step(t, m, result)
	if m.busy {
		t.Fatalf("the lobby should be idle again")
	}
	if !strings.Contains(m.note, "running") {
		t.Fatalf("note = %q, want the result text", m.note)
	}
	if cmd == nil {
		t.Fatalf("a finished action should refresh the instance list")
	}
}

func TestBusyRefusesASecondAction(t *testing.T) {
	t.Parallel()
	r := &recorder{items: []Item{{Name: "Partidita"}}}
	m := newLobby(t, r)
	m, _ = step(t, m, listMsg{items: r.items})
	m, cmd := step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	drain(t, cmd) // the first start reaches the backend and records the call

	m, cmd = step(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, cmd = step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("a second action ran while the first was in flight")
	}
	if r.called != "start" {
		t.Fatalf("backend called %q, want the first start", r.called)
	}
}

func TestFailureKeepsTheLobbyUsable(t *testing.T) {
	t.Parallel()
	r := &recorder{items: []Item{{Name: "Partidita"}}, runErr: errors.New("instance is running, stop it first")}
	m := newLobby(t, r)
	m, _ = step(t, m, listMsg{items: r.items})
	m, _ = step(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, cmd := step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	m, _ = step(t, m, message[resultMsg](t, drain(t, cmd)))
	if !m.bad {
		t.Fatalf("a failed action should be reported as an error")
	}
	if !strings.Contains(m.note, "stop it first") {
		t.Fatalf("note = %q, want the backend error", m.note)
	}
	if m.screen != screenLobby || len(m.items) != 1 {
		t.Fatalf("a failed action must not lose the lobby state")
	}
}

func TestLogsScreenReadsAndReturns(t *testing.T) {
	t.Parallel()
	r := &recorder{
		items: []Item{{Name: "Partidita"}},
		logs:  []string{"line one", "line two"},
	}
	m := newLobby(t, r)
	m, _ = step(t, m, listMsg{items: r.items})
	m.cursor = 2 // Logs

	m, cmd := step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screen != screenLogs {
		t.Fatalf("screen = %d, want the log screen", m.screen)
	}

	m, _ = step(t, m, message[logsMsg](t, drain(t, cmd)))
	if r.called != "logs" || r.name != "Partidita" {
		t.Fatalf("backend called %q on %q, want logs on Partidita", r.called, r.name)
	}
	if len(m.logs) != 2 || m.busy {
		t.Fatalf("logs = %d lines, busy = %v", len(m.logs), m.busy)
	}

	m, _ = step(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.screen != screenLobby {
		t.Fatalf("esc did not return to the lobby")
	}
}

func TestLogScrollStaysInsideTheBuffer(t *testing.T) {
	t.Parallel()
	m := newLobby(t, &recorder{})
	m.height = 20
	m.logs = make([]string, 50)

	m = m.scrollLogs(-10)
	if m.scroll != 0 {
		t.Fatalf("scroll = %d, want 0", m.scroll)
	}
	m = m.scrollLogs(1000)
	if want := len(m.logs) - m.logRows(); m.scroll != want {
		t.Fatalf("scroll = %d, want %d", m.scroll, want)
	}
}

func TestBackendValidation(t *testing.T) {
	t.Parallel()
	if err := (Backend{}).validate(); err == nil {
		t.Fatalf("an empty backend should be refused")
	}
	full := (&recorder{}).backend()
	if err := full.validate(); err != nil {
		t.Fatalf("a complete backend should pass: %v", err)
	}
}
