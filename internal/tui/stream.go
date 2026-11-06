package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// streamEventMsg carries one step of a streamed operation. The sequence number
// identifies the channel it came from, so a cancelled stream cannot feed a
// screen that already started a new one.
type streamEventMsg struct {
	seq   int
	event Event
}

// streamClosedMsg reports that a streamed operation closed its channel.
type streamClosedMsg struct{ seq int }

// streamStartMsg asks the file screen to start one operation after a
// confirmation.
type streamStartMsg struct{ update bool }

// files installs or updates the server files and streams the download.
type files struct {
	ctx     context.Context
	backend FileBackend
	item    Item

	option  int
	running bool
	cancel  context.CancelFunc
	source  <-chan Event
	seq     int

	lines  []string
	err    error
	done   bool
	scroll int
	follow bool

	width  int
	height int
}

// newFiles returns the install and update screen of one instance.
func newFiles(ctx context.Context, backend FileBackend, item Item) screen {
	return &files{ctx: ctx, backend: backend, item: item, follow: true}
}

// Init shows the menu; nothing starts until the user asks.
func (f *files) Init() tea.Cmd { return nil }

// Update runs the chosen operation and folds its output.
func (f *files) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		f.width, f.height = msg.Width, msg.Height
		return f, nil

	case tea.KeyPressMsg:
		return f, f.key(msg)

	case streamStartMsg:
		return f, f.start(msg.update)

	case streamEventMsg:
		if msg.seq != f.seq {
			return f, nil
		}
		if msg.event.Line != "" {
			f.lines = appendLines(f.lines, msg.event.Line)
			if f.follow {
				f.scroll = 0
			}
		}
		if msg.event.Err != nil {
			f.err = msg.event.Err
		}
		if msg.event.Done {
			f.done, f.running = true, false
			return f, nil
		}
		return f, f.next()

	case streamClosedMsg:
		if msg.seq == f.seq {
			f.done, f.running = true, false
		}
		return f, nil

	case resumeMsg:
		return f, nil
	}
	return f, nil
}

// key handles the menu, the stream and the scroll keys.
func (f *files) key(msg tea.KeyPressMsg) tea.Cmd {
	if !f.running && !f.done && len(f.lines) == 0 {
		switch msg.String() {
		case "esc", "q":
			return pop()
		case "up", "k":
			f.option = 0
		case "down", "j":
			f.option = 1
		case "enter", " ":
			if f.option == 1 {
				item := f.item.Name
				return askConfirm("Update "+item+"?", "The server files are replaced. The world is archived first and the server must be stopped.", func() tea.Msg {
					return streamStartMsg{update: true}
				})
			}
			return func() tea.Msg { return streamStartMsg{} }
		}
		return nil
	}

	switch msg.String() {
	case "esc":
		if f.running {
			f.stop()
			f.running, f.done = false, false
			return report("cancelled %s", f.item.Name)
		}
		return pop()
	case "up", "pgup":
		f.scrollBy(f.rows())
	case "down", "pgdown":
		f.scrollBy(-f.rows())
	case "g":
		f.scroll = max(0, len(f.lines)-1)
		f.follow = false
	case "G":
		f.scroll, f.follow = 0, true
	}
	return nil
}

// start launches the selected operation in its own context.
func (f *files) start(update bool) tea.Cmd {
	if f.running {
		return nil
	}

	child, cancel := context.WithCancel(f.ctx)
	f.cancel = cancel
	f.running, f.done, f.follow = true, false, true
	f.err, f.lines, f.scroll = nil, nil, 0

	if update {
		f.option = 1
		f.source = f.backend.Update(child, f.item.Name)
	} else {
		f.option = 0
		f.source = f.backend.Install(child, f.item.Name)
	}
	f.seq++
	return f.next()
}

// stop cancels the operation still streaming.
func (f *files) stop() {
	if f.cancel != nil {
		f.cancel()
		f.cancel = nil
	}
}

// next reads one event from the stream, one command per event.
func (f *files) next() tea.Cmd {
	source, seq := f.source, f.seq
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

// rows is how many lines fit on screen.
func (f *files) rows() int { return streamRows(f.height) }

// scrollBy moves the window, never past the oldest line.
func (f *files) scrollBy(delta int) {
	f.scroll = min(max(0, f.scroll+delta), max(0, len(f.lines)-1))
	if f.scroll == 0 {
		f.follow = true
	}
}

// View draws the menu before a run and the stream afterwards.
func (f *files) View(width, height int) string {
	// Key handling needs to know how much fits, and View is the only place
	// that sees the real size.
	f.width, f.height = width, height
	if !f.running && !f.done && len(f.lines) == 0 {
		return clipBlock(box(f.menu(), width), width)
	}

	head := titleStyle.Render("Files · "+f.item.Name) + "   " + dimStyle.Render(f.command())

	rows := streamRows(height)
	visible := tailWindow(f.lines, rows, f.scroll)
	lines := make([]string, 0, len(visible))
	for _, line := range visible {
		lines = append(lines, clip(line, width))
	}
	body := head + "\n\n" + strings.Join(lines, "\n")
	if len(lines) == 0 {
		body += dimStyle.Render("waiting for the first line…")
	}

	body += "\n\n" + f.status()
	return clipBlock(body, width)
}

// menu draws the two operations.
func (f *files) menu() string {
	options := []struct {
		label string
		hint  string
	}{
		{"Install", "download the server files with SteamCMD"},
		{"Update", "replace the server files, archiving the world first"},
	}

	lines := make([]string, 0, len(options))
	for i, option := range options {
		if i == f.option {
			lines = append(lines, selectedStyle.Render("› "+option.label)+"  "+dimStyle.Render(option.hint))
			continue
		}
		lines = append(lines, "  "+option.label+"  "+dimStyle.Render(option.hint))
	}

	head := titleStyle.Render("Files · " + f.item.Name)
	body := head + "\n\n" + strings.Join(lines, "\n")
	body += "\n\n" + dimStyle.Render("↑↓ choose    enter run    esc back    ? help")
	return body
}

// command is the command line equivalent of the running operation.
func (f *files) command() string {
	if f.option == 1 {
		return "gamehost update " + f.item.Name
	}
	return "gamehost install " + f.item.Name
}

// status is the line under the stream.
func (f *files) status() string {
	switch {
	case f.err != nil:
		return errorStyle.Render(f.err.Error()) + dimStyle.Render("    esc back")
	case f.running:
		return spinner[0] + dimStyle.Render(" running…    esc cancels")
	case f.done:
		return okStyle.Render("done") + dimStyle.Render("    ↑↓ scroll    esc back")
	default:
		return dimStyle.Render("esc back")
	}
}

// Keys describes the file screen shortcuts.
func (f *files) Keys() []KeyHelp {
	return []KeyHelp{
		{"enter", "install or update", "gamehost install | update"},
		{"↑ ↓", "choose or scroll", ""},
		{"esc", "cancel or go back", ""},
	}
}
