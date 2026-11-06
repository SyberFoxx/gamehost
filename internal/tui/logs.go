package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// logs shows the newest tail of one log and follows it live.
type logs struct {
	ctx     context.Context
	cancel  context.CancelFunc
	backend LogBackend
	name    string

	source <-chan Event
	lines  []string
	err    error
	done   bool
	scroll int
	follow bool

	width  int
	height int
}

// newLogs returns the live log of one instance.
func newLogs(ctx context.Context, backend LogBackend, name string) screen {
	child, cancel := context.WithCancel(ctx)
	return &logs{ctx: child, cancel: cancel, backend: backend, name: name, follow: true}
}

// Init starts the follower, which tails first and then follows.
func (l *logs) Init() tea.Cmd {
	l.source = l.backend.Follow(l.ctx, l.name, DefaultLogLines)
	return l.next()
}

// Update folds streamed lines and keeps the view on the live edge while
// following.
func (l *logs) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		return l, nil

	case streamEventMsg:
		if msg.event.Line != "" {
			l.lines = appendLines(l.lines, msg.event.Line)
			if l.follow {
				l.scroll = 0
			}
		}
		if msg.event.Err != nil {
			l.err = msg.event.Err
		}
		if msg.event.Done {
			l.done = true
			return l, nil
		}
		return l, l.next()

	case streamClosedMsg:
		l.done = true
		return l, nil

	case tea.KeyPressMsg:
		return l, l.key(msg)

	case resumeMsg:
		return l, nil
	}
	return l, nil
}

// key stops the follower or scrolls through the buffer.
func (l *logs) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		if l.cancel != nil {
			l.cancel()
			l.cancel = nil
		}
		return pop()
	case "up", "pgup":
		l.follow = false
		l.scrollBy(l.rows())
	case "down", "pgdown":
		l.scrollBy(-l.rows())
	case "g":
		l.follow = false
		l.scroll = max(0, len(l.lines)-1)
	case "G":
		l.scroll, l.follow = 0, true
	}
	return nil
}

// next reads one event from the follower, one command per event.
func (l *logs) next() tea.Cmd {
	source := l.source
	if source == nil {
		return func() tea.Msg { return streamClosedMsg{} }
	}
	return func() tea.Msg {
		event, ok := <-source
		if !ok {
			return streamClosedMsg{}
		}
		return streamEventMsg{event: event}
	}
}

// scrollBy moves the window and returns to the live edge at the bottom.
func (l *logs) scrollBy(delta int) {
	l.scroll = min(max(0, l.scroll+delta), max(0, len(l.lines)-1))
	if l.scroll == 0 {
		l.follow = true
	}
}

// rows is how many log lines fit on screen.
func (l *logs) rows() int { return streamRows(l.height) }

// View draws the window and the follower status.
func (l *logs) View(width, height int) string {
	// Key handling needs to know how much fits, and View is the only place
	// that sees the real size.
	l.width, l.height = width, height
	head := titleStyle.Render("Logs · "+l.name) + "   " + dimStyle.Render("gamehost logs "+l.name+" -f")

	rows := streamRows(height)
	visible := tailWindow(l.lines, rows, l.scroll)
	lines := make([]string, 0, len(visible))
	for _, line := range visible {
		lines = append(lines, clip(line, width))
	}

	body := head + "\n\n" + strings.Join(lines, "\n")
	if len(lines) == 0 {
		switch {
		case l.err != nil:
			body += errorStyle.Render(l.err.Error())
		case l.done:
			body += dimStyle.Render("no logs yet, start the server first")
		default:
			body += dimStyle.Render("waiting for the first line…")
		}
	}

	body += "\n\n" + l.status()
	return clipBlock(body, width)
}

// status is the line under the log.
func (l *logs) status() string {
	switch {
	case l.err != nil:
		return errorStyle.Render(l.err.Error()) + dimStyle.Render("    esc back")
	case l.follow:
		return spinner[0] + dimStyle.Render(" following    ↑ scroll    esc stop")
	default:
		return dimStyle.Render("paused    ↓ follow    esc stop")
	}
}

// Keys describes the log shortcuts.
func (l *logs) Keys() []KeyHelp {
	return []KeyHelp{
		{"↑ ↓", "scroll the log", ""},
		{"G", "jump to the live edge", ""},
		{"esc", "stop following and go back", "gamehost logs -f"},
	}
}
