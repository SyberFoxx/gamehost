package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// panelMsg reports the panel that started, with the function that stops it.
type panelMsg struct {
	address string
	stop    func()
	err     error
}

// panel starts and stops the browser panel from inside the lobby.
type panel struct {
	ctx     context.Context
	backend PanelBackend

	addr    textinput.Model
	address string
	stop    func()
	boot    []choice
	bootAt  int
	busy    bool
	err     string
}

// newPanel returns the web panel screen. The instances become the choices of
// the boot start, the lobby's version of `serve --start`.
func newPanel(ctx context.Context, backend PanelBackend, items []Item) screen {
	addr := textinput.New()
	addr.Prompt = ""
	addr.Placeholder = "127.0.0.1:8080"
	addr.CharLimit = 64
	addr.SetValue("127.0.0.1:8080")

	boot := []choice{{value: "", label: "none"}}
	for _, item := range items {
		boot = append(boot, choice{value: item.Name, label: item.Name})
	}
	return &panel{ctx: ctx, backend: backend, addr: addr, boot: boot}
}

// Init focuses the address field.
func (p *panel) Init() tea.Cmd { return p.addr.Focus() }

// Update starts, stops or edits the listen address.
func (p *panel) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.addr.SetWidth(max(12, msg.Width-24))
		return p, nil

	case tea.KeyPressMsg:
		return p, p.key(msg)

	case tea.PasteMsg:
		if p.stop != nil {
			return p, nil
		}
		var cmd tea.Cmd
		p.addr, cmd = p.addr.Update(msg)
		return p, cmd

	case panelMsg:
		p.busy = false
		if msg.err != nil {
			p.err = msg.err.Error()
			return p, reportErr(msg.err)
		}
		p.err = ""
		p.address, p.stop = msg.address, msg.stop
		p.addr.Blur()
		return p, report("serving http://%s", msg.address)

	case resumeMsg:
		return p, nil
	}
	return p, nil
}

// key handles the start, stop, address editing and boot instance.
func (p *panel) key(msg tea.KeyPressMsg) tea.Cmd {
	if p.stop != nil {
		switch msg.String() {
		case "esc", "q":
			p.shutdown()
			return pop()
		case "s":
			p.shutdown()
			return report("panel stopped")
		}
		return nil
	}

	switch msg.String() {
	case "esc", "q":
		return pop()
	case "enter", "s":
		return p.start()
	case "b":
		if len(p.boot) > 1 {
			p.bootAt = (p.bootAt + 1) % len(p.boot)
		}
		return nil
	}

	var cmd tea.Cmd
	p.addr, cmd = p.addr.Update(msg)
	return cmd
}

// start listens and serves the panel until the lobby leaves.
func (p *panel) start() tea.Cmd {
	if p.busy || p.stop != nil {
		return nil
	}
	addr := strings.TrimSpace(p.addr.Value())
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	p.busy = true
	backend, ctx, boot := p.backend, p.ctx, p.bootName()
	return func() tea.Msg {
		address, stop, err := backend.Start(ctx, addr, boot)
		return panelMsg{address: address, stop: stop, err: err}
	}
}

// bootName is the instance the panel starts, or empty for none.
func (p *panel) bootName() string {
	if p.bootAt <= 0 || p.bootAt >= len(p.boot) {
		return ""
	}
	return p.boot[p.bootAt].value
}

// shutdown stops the listener the screen owns.
func (p *panel) shutdown() {
	if p.stop != nil {
		p.stop()
	}
	p.stop = nil
	p.address = ""
	p.addr.Focus()
}

// View draws the address or the live panel.
func (p *panel) View(width, _ int) string {
	p.addr.SetWidth(max(12, width-24))
	head := titleStyle.Render("Web panel") + "   " + dimStyle.Render("gamehost serve")

	var body string
	switch {
	case p.stop != nil:
		body = head + "\n\n" +
			okStyle.Render("serving http://"+p.address) + "\n\n" +
			dimStyle.Render("the panel only listens on this machine; put a reverse proxy in front of it")
		if boot := p.bootName(); boot != "" {
			body += "\n" + dimStyle.Render("started with the panel: ") + boot
		}
	case p.busy:
		body = head + "\n\n" + dimStyle.Render("starting…")
	default:
		body = head + "\n\n" +
			dimStyle.Render("address  ") + p.addr.View() + "\n" +
			dimStyle.Render("start    ") + p.bootChoice() + "\n\n" +
			dimStyle.Render("the panel has no authentication: keep it on loopback")
	}

	if p.err != "" {
		body += "\n\n" + errorStyle.Render(p.err)
	}
	body += "\n\n" + p.footer()
	return clipBlock(box(body, width), width)
}

// bootChoice renders the selected boot instance.
func (p *panel) bootChoice() string {
	if len(p.boot) == 0 {
		return dimStyle.Render("(none)")
	}
	return dimStyle.Render("‹ ") + p.boot[p.bootAt].text() + dimStyle.Render(" ›")
}

// footer is the key line of the panel screen.
func (p *panel) footer() string {
	if p.stop != nil {
		return dimStyle.Render("s stop    esc back (stops the panel)")
	}
	if p.busy {
		return dimStyle.Render("starting…")
	}
	if len(p.boot) > 1 {
		return dimStyle.Render("enter start    b start with    esc back    ? help")
	}
	return dimStyle.Render("enter start    esc back    ? help")
}

// Keys describes the panel shortcuts.
func (p *panel) Keys() []KeyHelp {
	return []KeyHelp{
		{"enter", "start the panel and the backup schedule", "gamehost serve"},
		{"b", "instance started with the panel", "gamehost serve --start"},
		{"s", "stop the panel", ""},
		{"esc", "back, stopping it", ""},
	}
}

// editingText tells the root that ? belongs to the address input.
func (p *panel) editingText() bool { return p.stop == nil }
