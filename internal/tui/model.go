package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// screen is one page of the lobby. Three methods keeps the contract inside the
// project rule for small interfaces: the root owns navigation and each screen
// owns its own state and drawing.
type screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (screen, tea.Cmd)
	View(width, height int) string
}

// keyser is a screen that can describe its own keys for the help overlay.
type keyser interface {
	Keys() []KeyHelp
}

// textScreener is a screen editing text, where ? is a character and not the
// help shortcut.
type textScreener interface {
	editingText() bool
}

// KeyHelp is one row of the help overlay.
type KeyHelp struct {
	Key    string
	Action string
	CLI    string
}

// pushMsg adds a screen to the stack.
type pushMsg struct{ screen screen }

// popMsg removes the top screen.
type popMsg struct{}

// resumeMsg tells a screen it is visible again after a child popped.
type resumeMsg struct{}

// quitMsg asks the root to leave the program.
type quitMsg struct{}

// statusMsg is one line of feedback for the bottom of the screen.
type statusMsg struct {
	text string
	err  bool
}

// tickMsg advances a spinner.
type tickMsg time.Time

// listMsg carries a reloaded instance list.
type listMsg struct {
	items []Item
	err   error
}

// resultMsg carries the outcome of one blocking server action.
type resultMsg struct {
	text string
	err  error
}

// detailMsg carries the full configuration of one instance.
type detailMsg struct {
	detail Detail
	err    error
}

// settingsMsg carries the editable settings of one instance.
type settingsMsg struct {
	settings Settings
	err      error
}

// confirmMsg asks the root to show a modal question before running cmd.
type confirmMsg struct {
	question string
	detail   string
	yes      tea.Cmd
}

// confirmation is a modal question that replaces the screen until answered.
type confirmation struct {
	question string
	detail   string
	yes      tea.Cmd
}

// Model is the root of the lobby: it owns the screen stack, the bottom note,
// the help overlay and the confirm dialog.
type Model struct {
	ctx     context.Context
	backend Backend

	stack []screen

	width  int
	height int

	note string
	bad  bool

	help    bool
	confirm *confirmation
}

// newModel returns a lobby showing the instance list.
func newModel(ctx context.Context, backend Backend) Model {
	return Model{
		ctx:     ctx,
		backend: backend,
		stack:   []screen{newLobby(ctx, backend)},
	}
}

// Init starts the visible screen.
func (m Model) Init() tea.Cmd {
	return m.top().Init()
}

// Update routes one message: navigation and global keys stay here, everything
// else belongs to the screen on top of the stack.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// Screens keep their own width for text inputs and scroll windows, so
		// the size must reach them too.
		return m.forward(msg)

	case pushMsg:
		m.stack = append(m.stack, msg.screen)
		m.note, m.bad = "", false
		return m, msg.screen.Init()

	case popMsg:
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
		}
		return m, resumeChild()

	case resumeMsg:
		return m.forward(msg)

	case statusMsg:
		m.note, m.bad = oneLine(msg.text), msg.err
		return m, nil

	case confirmMsg:
		m.confirm = &confirmation{question: msg.question, detail: msg.detail, yes: msg.yes}
		return m, nil

	case quitMsg:
		return m, quitProgram()

	case tea.KeyPressMsg:
		return m.key(msg)

	default:
		return m.forward(msg)
	}
}

// key handles the keys the root owns before any screen sees them.
func (m Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if m.confirm != nil {
		return m.confirmKey(key)
	}
	if m.help {
		if key == "?" || key == "esc" || key == "q" {
			m.help = false
		}
		return m, nil
	}

	switch key {
	case "ctrl+c":
		return m, quitProgram()
	case "?":
		// While a form edits text, ? is a character and must reach the input.
		if s, ok := m.top().(textScreener); ok && s.editingText() {
			break
		}
		m.help = true
		return m, nil
	}
	return m.forward(msg)
}

// confirmKey answers the modal question.
func (m Model) confirmKey(key string) (tea.Model, tea.Cmd) {
	answer := m.confirm
	m.confirm = nil

	switch key {
	case "y", "Y", "enter":
		return m, answer.yes
	case "n", "N", "esc", "q":
		return m, nil
	}
	// Any other key keeps the question open.
	m.confirm = answer
	return m, nil
}

// forward hands one message to the visible screen and keeps its new state.
func (m Model) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	top, cmd := m.top().Update(msg)
	m.stack[len(m.stack)-1] = top
	return m, cmd
}

// top returns the visible screen.
func (m Model) top() screen {
	return m.stack[len(m.stack)-1]
}

// bodyHeight is how much room the visible screen gets, leaving a line for the
// bottom note.
func (m Model) bodyHeight() int {
	if m.height <= 0 {
		return 0
	}
	return max(8, m.height-1)
}

// View renders the lobby in the alternate screen.
func (m Model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	view.WindowTitle = "gamehost"
	return view
}

// render draws the modal, the help or the visible screen plus the note.
func (m Model) render() string {
	if m.confirm != nil {
		return m.renderConfirm()
	}
	if m.help {
		return m.renderHelp()
	}

	body := m.top().View(m.width, m.bodyHeight())
	if m.note == "" {
		return body + "\n"
	}

	style := dimStyle
	if m.bad {
		style = errorStyle
	}
	return body + "\n" + style.Render(clip(m.note, m.width))
}

// askConfirm asks the root to show a modal question whose yes answer runs cmd.
func askConfirm(question, detail string, yes tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		return confirmMsg{question: question, detail: detail, yes: yes}
	}
}

// renderConfirm draws the modal question.
func (m Model) renderConfirm() string {
	body := titleStyle.Render(m.confirm.question) + "\n\n" +
		dimStyle.Render(m.confirm.detail) + "\n\n" +
		selectedStyle.Render("y") + " confirm    " +
		dimStyle.Render("n") + " cancel"
	return m.panel(body)
}

// renderHelp draws the key map of the visible screen plus the global keys.
func (m Model) renderHelp() string {
	keys := []KeyHelp{
		{"↑ ↓", "move", ""},
		{"← →", "change a value or instance", ""},
		{"enter", "open or run", ""},
		{"esc", "back", ""},
		{"?", "close this help", ""},
		{"ctrl+c", "leave the lobby", ""},
	}
	if k, ok := m.top().(keyser); ok {
		keys = append(k.Keys(), keys...)
	}

	keyWidth, actionWidth := 0, 0
	for _, k := range keys {
		keyWidth = max(keyWidth, textWidth(k.Key))
		actionWidth = max(actionWidth, textWidth(k.Action))
	}

	rows := make([]string, 0, len(keys))
	for _, k := range keys {
		row := titleStyle.Render(padRight(k.Key, keyWidth)) + "  " + padRight(k.Action, actionWidth)
		if k.CLI != "" {
			row += "  " + dimStyle.Render(k.CLI)
		}
		rows = append(rows, row)
	}

	head := titleStyle.Render("Keys") + "   " + dimStyle.Render("? closes")
	return m.panel(head + "\n\n" + strings.Join(rows, "\n"))
}

// panel wraps help and modal content in the same frame as the rest.
func (m Model) panel(content string) string {
	style := panelStyle
	if m.width > 12 {
		style = style.Width(min(76, m.width-4))
	}
	return style.Render(content)
}

// push adds a screen to the stack.
func push(s screen) tea.Cmd {
	return func() tea.Msg { return pushMsg{screen: s} }
}

// pop removes the top screen and tells the new top it is visible again.
func pop() tea.Cmd {
	return func() tea.Msg { return popMsg{} }
}

// resumeChild asks the newly visible screen to refresh.
func resumeChild() tea.Cmd {
	return func() tea.Msg { return resumeMsg{} }
}

// requestQuit leaves the program.
func requestQuit() tea.Cmd {
	return func() tea.Msg { return quitMsg{} }
}

// quitProgram asks the runtime to leave the alternate screen cleanly.
func quitProgram() tea.Cmd {
	return func() tea.Msg { return tea.Quit() }
}

// report publishes one line of feedback.
func report(format string, args ...any) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: fmt.Sprintf(format, args...)} }
}

// reportErr publishes a failure.
func reportErr(err error) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: err.Error(), err: true} }
}

// tick advances a spinner without leaving a timer behind.
func tick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// oneLine folds a multi-line report into a single footer line, so a backend
// that returns a whole paragraph cannot break the layout.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
