package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// screen is the view the lobby is showing.
type screen int

const (
	screenLobby screen = iota
	screenLogs
)

// action is one entry of the lobby menu.
type action int

const (
	actionStart action = iota
	actionStop
	actionLogs
	actionBackup
	actionStatus
	actionQuit
)

// menu is the ordered list of actions the lobby offers.
// Every entry names what it does, because the point of the lobby is teaching.
var menu = []struct {
	action action
	label  string
	hint   string
}{
	{actionStart, "Start", "run it in the background"},
	{actionStop, "Stop", "wait for a clean save"},
	{actionLogs, "Logs", "read the newest log"},
	{actionBackup, "Backup", "archive the world"},
	{actionStatus, "Status", "refresh phase, pid and join code"},
	{actionQuit, "Quit", "leave the lobby"},
}

// spinner is drawn while a backend call is in flight.
var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// tickMsg advances the spinner.
type tickMsg time.Time

// listMsg carries the result of Backend.List.
type listMsg struct {
	items []Item
	err   error
}

// resultMsg carries the outcome of a start, stop or backup.
type resultMsg struct {
	action action
	text   string
	err    error
}

// logsMsg carries the newest log lines of one instance.
type logsMsg struct {
	lines []string
	err   error
}

// Model is the state of the lobby.
type Model struct {
	ctx     context.Context
	backend Backend

	items  []Item
	item   int
	cursor int

	busy  bool
	frame int
	bad   bool
	note  string

	screen screen
	logs   []string
	scroll int

	width  int
	height int
}

// newModel returns a lobby ready to load its instances.
func newModel(ctx context.Context, backend Backend) Model {
	return Model{ctx: ctx, backend: backend}
}

// Init loads the instance list.
func (m Model) Init() tea.Cmd {
	return m.list()
}

// Update routes one message to the screen that owns it.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		return m.key(msg.String())

	case tickMsg:
		if !m.busy {
			return m, nil
		}
		m.frame++
		return m, tick()

	case listMsg:
		m.busy = false
		if msg.err != nil {
			m.fail("list instances: " + msg.err.Error())
			return m, nil
		}
		m.items = msg.items
		m.clampItem()
		return m, nil

	case resultMsg:
		m.busy = false
		if msg.err != nil {
			m.fail(msg.err.Error())
			return m, m.list()
		}
		m.bad = false
		m.note = oneLine(msg.text)
		return m, m.list()

	case logsMsg:
		m.busy = false
		if msg.err != nil {
			m.fail(msg.err.Error())
			return m, nil
		}
		m.logs = msg.lines
		m.scroll = len(msg.lines)
		return m, nil
	}
	return m, nil
}

// key handles one keystroke for the current screen.
func (m Model) key(key string) (tea.Model, tea.Cmd) {
	if key == "ctrl+c" {
		return m, quit()
	}
	if m.screen == screenLogs {
		return m.logsKey(key)
	}
	return m.lobbyKey(key)
}

// lobbyKey handles one keystroke of the menu.
func (m Model) lobbyKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q":
		return m, quit()
	case "up", "k":
		m.cursor = max(0, m.cursor-1)
	case "down", "j":
		m.cursor = min(len(menu)-1, m.cursor+1)
	case "left", "h":
		if m.item > 0 {
			m.item--
		}
	case "right", "l":
		if m.item < len(m.items)-1 {
			m.item++
		}
	case "enter", " ":
		return m.activate()
	case "r":
		if m.busy {
			return m, nil
		}
		m.busy, m.bad, m.note = true, false, "refreshing…"
		return m, tea.Batch(m.list(), tick())
	}
	return m, nil
}

// logsKey handles one keystroke of the log screen.
func (m Model) logsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q", "backspace":
		m.screen = screenLobby
		m.logs = nil
		m.busy = false
		m.note = ""
	case "up", "k":
		m = m.scrollLogs(-1)
	case "down", "j":
		m = m.scrollLogs(1)
	case "pgup":
		m = m.scrollLogs(-m.logRows())
	case "pgdown":
		m = m.scrollLogs(m.logRows())
	case "r":
		if m.busy {
			return m, nil
		}
		item, ok := m.current()
		if !ok {
			return m, nil
		}
		m.busy = true
		return m, tea.Batch(m.fetchLogs(item.Name), tick())
	}
	return m, nil
}

// activate runs the selected menu entry against the selected instance.
func (m Model) activate() (tea.Model, tea.Cmd) {
	chosen := menu[m.cursor]
	if chosen.action == actionQuit {
		return m, quit()
	}

	item, ok := m.current()
	if !ok {
		m.fail("no instance yet: create one with gamehost create")
		return m, nil
	}
	if m.busy {
		return m, nil
	}

	switch chosen.action {
	case actionStatus:
		m.busy, m.bad, m.note = true, false, "refreshing "+item.Name+"…"
		return m, tea.Batch(m.list(), tick())
	case actionLogs:
		m.screen = screenLogs
		m.logs = nil
		m.scroll = 0
		m.busy, m.bad, m.note = true, false, ""
		return m, tea.Batch(m.fetchLogs(item.Name), tick())
	}

	m.busy, m.bad = true, false
	m.note = chosen.label + " " + item.Name + "…"
	return m, tea.Batch(m.run(chosen.action, item.Name), tick())
}

// run turns one action into a tea.Cmd that the runtime runs off the UI loop.
func (m Model) run(chosen action, name string) tea.Cmd {
	backend, ctx := m.backend, m.ctx
	return func() tea.Msg {
		var text string
		var err error
		switch chosen {
		case actionStart:
			text, err = backend.Start(ctx, name)
		case actionStop:
			text, err = backend.Stop(ctx, name)
		case actionBackup:
			text, err = backend.Backup(ctx, name)
		}
		return resultMsg{action: chosen, text: text, err: err}
	}
}

// list reloads the instance list without blocking the UI.
func (m Model) list() tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		items, err := backend.List()
		return listMsg{items: items, err: err}
	}
}

// fetchLogs reads the newest log of one instance.
func (m Model) fetchLogs(name string) tea.Cmd {
	backend := m.backend
	return func() tea.Msg {
		lines, err := backend.Logs(name, DefaultLogLines)
		return logsMsg{lines: lines, err: err}
	}
}

// tick advances the spinner without leaving a timer behind.
func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// quit asks the runtime to leave the alternate screen cleanly.
func quit() tea.Cmd {
	return func() tea.Msg { return tea.Quit() }
}

// fail records an inline error that keeps the lobby usable.
func (m *Model) fail(note string) {
	m.bad = true
	m.note = oneLine(note)
}

// oneLine folds a multi-line report into a single footer line, so a backend
// that returns a whole paragraph cannot break the layout.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// clampItem keeps the instance selection inside the list.
func (m *Model) clampItem() {
	if len(m.items) == 0 {
		m.item = 0
		return
	}
	m.item = min(max(0, m.item), len(m.items)-1)
}

// current returns the selected instance, if any.
func (m Model) current() (Item, bool) {
	if m.item < 0 || m.item >= len(m.items) {
		return Item{}, false
	}
	return m.items[m.item], true
}

// logRows is how many log lines fit between the header and the footer.
func (m Model) logRows() int {
	return max(3, m.height-8)
}

// scrollLogs moves the log window and keeps it inside the buffer.
func (m Model) scrollLogs(delta int) Model {
	last := max(0, len(m.logs)-m.logRows())
	m.scroll = min(max(0, m.scroll+delta), last)
	return m
}
