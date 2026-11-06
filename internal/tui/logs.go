package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// logs shows the newest tail of one log and follows it live.
type logs struct {
	parent  context.Context
	ctx     context.Context
	cancel  context.CancelFunc
	backend LogBackend
	name    string

	source <-chan Event
	seq    int

	lines   []string
	tail    int
	editing bool
	input   textinput.Model

	err    error
	done   bool
	scroll int
	follow bool

	width  int
	height int
}

// newLogs returns the live log of one instance.
func newLogs(ctx context.Context, backend LogBackend, name string) screen {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = "100"
	input.CharLimit = 7

	child, cancel := context.WithCancel(ctx)
	return &logs{
		parent:  ctx,
		ctx:     child,
		cancel:  cancel,
		backend: backend,
		name:    name,
		tail:    DefaultLogLines,
		input:   input,
		follow:  true,
	}
}

// Init starts the follower, which tails first and then follows.
func (l *logs) Init() tea.Cmd {
	return l.start()
}

// start replaces the follower with one that replays the current tail size.
// The sequence number retires the messages of the previous channel, so a
// cancelled follower cannot close the new one.
func (l *logs) start() tea.Cmd {
	if l.cancel != nil {
		l.cancel()
	}
	l.ctx, l.cancel = context.WithCancel(l.parent)
	l.source = l.backend.Follow(l.ctx, l.name, l.tail)
	l.seq++

	l.lines, l.err, l.done, l.scroll, l.follow = nil, nil, false, 0, true
	return l.next()
}

// Update folds streamed lines and keeps the view on the live edge while
// following.
func (l *logs) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		l.input.SetWidth(max(12, msg.Width-24))
		return l, nil

	case streamEventMsg:
		if msg.seq != l.seq {
			return l, nil
		}
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
		if msg.seq == l.seq {
			l.done = true
		}
		return l, nil

	case tea.KeyPressMsg:
		return l, l.key(msg)

	case resumeMsg:
		return l, nil
	}
	return l, nil
}

// key edits the tail size, stops the follower or scrolls the buffer.
func (l *logs) key(msg tea.KeyPressMsg) tea.Cmd {
	if l.editing {
		switch msg.String() {
		case "esc":
			l.editing = false
			l.input.Blur()
			return nil
		case "enter":
			return l.applyTail()
		}
		var cmd tea.Cmd
		l.input, cmd = l.input.Update(msg)
		return cmd
	}

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
	case "n":
		l.editing = true
		l.input.SetValue(strconv.Itoa(l.tail))
		return l.input.Focus()
	}
	return nil
}

// applyTail reloads the log with the tail size the user typed.
func (l *logs) applyTail() tea.Cmd {
	value := strings.TrimSpace(l.input.Value())
	count, err := strconv.Atoi(value)
	if err != nil || count <= 0 {
		return reportErr(fmt.Errorf("tail %q is not a positive number", value))
	}

	l.editing = false
	l.input.Blur()
	l.tail = count
	return tea.Batch(report("reading the last %d lines", count), l.start())
}

// next reads one event from the follower, one command per event.
func (l *logs) next() tea.Cmd {
	source, seq := l.source, l.seq
	if source == nil {
		return func() tea.Msg { return streamClosedMsg{seq: seq} }
	}
	return func() tea.Msg {
		event, ok := <-source
		if !ok {
			return streamClosedMsg{seq: seq}
		}
		return streamEventMsg{seq: seq, event: event}
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
	l.input.SetWidth(max(12, width-24))

	head := titleStyle.Render("Logs · "+l.name) + "   " + dimStyle.Render("gamehost logs "+l.name+" -f -n "+strconv.Itoa(l.tail))
	if l.editing {
		head = titleStyle.Render("Logs · "+l.name) + "   " + dimStyle.Render("tail  ") + l.input.View()
	}

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
	case l.editing:
		return dimStyle.Render("enter reload    esc cancel")
	case l.err != nil:
		return errorStyle.Render(l.err.Error()) + dimStyle.Render("    esc back")
	case l.follow:
		return spinner[0] + dimStyle.Render(" following    ↑ scroll    n lines    esc stop")
	default:
		return dimStyle.Render("paused    ↓ follow    n lines    esc stop")
	}
}

// Keys describes the log shortcuts.
func (l *logs) Keys() []KeyHelp {
	return []KeyHelp{
		{"↑ ↓", "scroll the log", ""},
		{"G", "jump to the live edge", ""},
		{"n", "reload the last N lines", "gamehost logs -n"},
		{"esc", "stop following and go back", "gamehost logs -f"},
	}
}

// editingText tells the root that ? belongs to the tail input.
func (l *logs) editingText() bool { return l.editing }
