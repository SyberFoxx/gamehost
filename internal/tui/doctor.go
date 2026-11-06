package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// reportMsg carries the doctor report.
type reportMsg struct {
	report Report
	err    error
}

// doctor shows the environment report of `gamehost doctor`.
type doctor struct {
	backend EnvBackend
	report  Report
	version string
	loaded  bool
	err     string
}

// newDoctor returns the environment screen.
func newDoctor(backend EnvBackend) screen {
	return &doctor{backend: backend}
}

// Init runs the checks and reads the build version.
func (d *doctor) Init() tea.Cmd {
	d.version = d.backend.Version()
	return d.load()
}

// load runs doctor away from the UI loop.
func (d *doctor) load() tea.Cmd {
	backend := d.backend
	return func() tea.Msg {
		report, err := backend.Doctor()
		return reportMsg{report: report, err: err}
	}
}

// Update folds the report.
func (d *doctor) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q":
			return d, pop()
		case "r":
			return d, d.load()
		}
		return d, nil

	case reportMsg:
		d.loaded = true
		if msg.err != nil {
			d.err = msg.err.Error()
			return d, reportErr(msg.err)
		}
		d.err = ""
		d.report = msg.report
		return d, nil

	case resumeMsg:
		return d, nil
	}
	return d, nil
}

// View draws every check with its status and the verdict.
func (d *doctor) View(width, _ int) string {
	head := titleStyle.Render("Environment") + "   " + dimStyle.Render("gamehost doctor")
	if d.version != "" {
		head += "   " + dimStyle.Render("gamehost "+d.version)
	}

	body := head + "\n\n"
	switch {
	case !d.loaded:
		body += dimStyle.Render("checking…")
	case len(d.report.Checks) == 0:
		body += dimStyle.Render("no checks ran")
	default:
		lines := make([]string, 0, len(d.report.Checks))
		for _, check := range d.report.Checks {
			marker, style := statusStyle(check.Status)
			line := style.Render(marker+" "+padRight(check.Name, 12)) + "  " + check.Detail
			lines = append(lines, clip(line, max(10, width-6)))
		}
		body += strings.Join(lines, "\n")
	}

	if d.err != "" {
		body += "\n\n" + errorStyle.Render(d.err)
	}
	if d.loaded {
		body += "\n\n" + d.verdict()
	}
	body += "\n\n" + dimStyle.Render("r rerun    esc back    ? help")
	return clipBlock(box(body, width), width)
}

// verdict states whether the environment can run servers.
func (d *doctor) verdict() string {
	if d.report.Healthy {
		return okStyle.Render("healthy") + dimStyle.Render("  this machine can run servers")
	}
	return errorStyle.Render("unhealthy") + dimStyle.Render("  fix the failed checks before starting a server")
}

// Keys describes the environment shortcuts.
func (d *doctor) Keys() []KeyHelp {
	return []KeyHelp{
		{"r", "run the checks again", "gamehost doctor"},
		{"esc", "back to the lobby", ""},
	}
}
