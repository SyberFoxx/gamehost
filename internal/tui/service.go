package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// serviceMsg carries the systemd state of one instance.
type serviceMsg struct {
	status ServiceStatus
	err    error
}

// uninstallServiceMsg asks the screen to remove the units the user confirmed.
type uninstallServiceMsg struct{}

// service shows and manages the systemd units of one instance.
type service struct {
	backend ServiceBackend
	item    Item

	status ServiceStatus
	loaded bool
	busy   bool
	err    string
}

// newService returns the systemd screen of one instance.
func newService(backend ServiceBackend, item Item) screen {
	return &service{backend: backend, item: item}
}

// Init reads the unit state.
func (s *service) Init() tea.Cmd { return s.load() }

// load reads the unit state away from the UI loop.
func (s *service) load() tea.Cmd {
	backend, name := s.backend, s.item.Name
	return func() tea.Msg {
		status, err := backend.Status(name)
		return serviceMsg{status: status, err: err}
	}
}

// Update folds the state and the result of an action.
func (s *service) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return s, s.key(msg)

	case resumeMsg:
		return s, s.load()

	case serviceMsg:
		s.busy = false
		s.loaded = true
		if msg.err != nil {
			s.err = msg.err.Error()
			return s, reportErr(msg.err)
		}
		s.err = ""
		s.status = msg.status
		return s, nil

	case resultMsg:
		s.busy = false
		if msg.err != nil {
			s.err = msg.err.Error()
			return s, reportErr(msg.err)
		}
		return s, tea.Batch(report("%s", msg.text), s.load())

	case uninstallServiceMsg:
		return s, s.runUninstall()
	}
	return s, nil
}

// key installs, enables or removes the units.
func (s *service) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		return pop()
	case "r":
		if s.busy {
			return nil
		}
		s.busy = true
		return s.load()
	case "i":
		return s.install(false)
	case "e":
		return s.install(true)
	case "u":
		return s.uninstall()
	}
	return nil
}

// install writes the units, optionally enabling and starting them now.
func (s *service) install(enable bool) tea.Cmd {
	if s.busy {
		return nil
	}
	s.busy = true
	backend, name := s.backend, s.item.Name
	return tea.Batch(
		report("installing units for %s…", name),
		func() tea.Msg {
			text, err := backend.Install(name, enable)
			return resultMsg{text: text, err: err}
		},
	)
}

// uninstall asks before removing the units.
func (s *service) uninstall() tea.Cmd {
	if s.busy {
		return nil
	}
	name := s.item.Name
	return askConfirm(
		"Uninstall the systemd units of "+name+"?",
		"The units are disabled and removed. The world and its configuration stay on disk.",
		func() tea.Msg { return uninstallServiceMsg{} },
	)
}

// runUninstall removes the units the user confirmed.
func (s *service) runUninstall() tea.Cmd {
	if s.busy {
		return nil
	}
	s.busy = true
	backend, name := s.backend, s.item.Name
	return func() tea.Msg {
		text, err := backend.Uninstall(name)
		return resultMsg{text: text, err: err}
	}
}

// View draws the unit names, the directory and the state.
func (s *service) View(width, _ int) string {
	head := titleStyle.Render("Systemd · "+s.item.Name) + "   " + dimStyle.Render("gamehost service")

	body := head + "\n\n"
	switch {
	case !s.loaded:
		body += dimStyle.Render("reading the unit state…")
	default:
		rows := [][2]string{
			{"unit", s.status.Unit},
			{"timer", orDash(s.status.Timer)},
			{"directory", orDash(s.status.UnitDir)},
			{"installed", yesNo(s.status.Installed)},
			{"active", yesNo(s.status.Active)},
		}
		lines := make([]string, 0, len(rows))
		for _, row := range rows {
			lines = append(lines, dimStyle.Render(padRight(row[0], 10))+"  "+row[1])
		}
		body += strings.Join(lines, "\n")
	}

	if s.err != "" {
		body += "\n\n" + errorStyle.Render(s.err)
	}
	body += "\n\n" + s.footer()
	return clipBlock(box(body, width), width)
}

// footer is the key line of the systemd screen.
func (s *service) footer() string {
	if s.busy {
		return dimStyle.Render("working…")
	}
	return dimStyle.Render("i install    e install and enable    u uninstall    r reload    esc back")
}

// orDash shows a dash for an empty value.
func orDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

// yesNo renders a boolean.
func yesNo(value bool) string {
	if value {
		return okStyle.Render("yes")
	}
	return dimStyle.Render("no")
}

// Keys describes the systemd shortcuts.
func (s *service) Keys() []KeyHelp {
	name := s.item.Name
	return []KeyHelp{
		{"i", "write the units", "gamehost service install " + name},
		{"e", "write and enable them", "gamehost service install " + name + " --enable"},
		{"u", "remove the units", "gamehost service uninstall " + name},
	}
}
