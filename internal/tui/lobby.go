package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// action is one entry of the lobby menu.
type action int

const (
	actStart action = iota
	actStop
	actLogs
	actBackup
	actDetails
	actSettings
	actFiles
	actAdmins
	actBackups
	actService
	actDoctor
	actPanel
	actCreate
	actQuit
)

// menuLine is one row of the menu; a header is a section title.
type menuLine struct {
	action action
	label  string
	hint   string
	header bool
}

// menuLines is the whole program, in the order a person reads it.
var menuLines = []menuLine{
	{header: true, label: "server"},
	{action: actStart, label: "Start", hint: "run it in the background"},
	{action: actStop, label: "Stop", hint: "wait for a clean save"},
	{action: actLogs, label: "Logs", hint: "read and follow the newest log"},
	{action: actBackup, label: "Backup", hint: "archive the world now"},
	{header: true, label: "instance"},
	{action: actDetails, label: "Details", hint: "phase, pid, join code, paths"},
	{action: actSettings, label: "Settings", hint: "world, players, network, backups"},
	{action: actFiles, label: "Files", hint: "install or update the server files"},
	{action: actAdmins, label: "Admins", hint: "who can run admin commands"},
	{action: actBackups, label: "Backups", hint: "list, restore or import a world"},
	{action: actService, label: "Systemd", hint: "run it with systemd units"},
	{header: true, label: "system"},
	{action: actDoctor, label: "Environment", hint: "check what the server needs"},
	{action: actPanel, label: "Web panel", hint: "serve the browser panel"},
	{action: actCreate, label: "New instance", hint: "create another world"},
	{action: actQuit, label: "Quit", hint: "leave the lobby"},
}

// lobby is the home screen: the instance list and the menu.
type lobby struct {
	ctx     context.Context
	backend Backend

	items  []Item
	item   int
	cursor int
	offset int

	width  int
	height int

	busy  bool
	frame int
}

// runActionMsg asks the lobby to start a blocking action after a confirmation.
type runActionMsg struct {
	kind action
	name string
}

// newLobby returns the home screen ready to load the instance list.
func newLobby(ctx context.Context, backend Backend) *lobby {
	l := &lobby{ctx: ctx, backend: backend}
	l.cursor = l.firstSelectable()
	return l
}

// Init loads the instances.
func (l *lobby) Init() tea.Cmd {
	return l.list()
}

// Update handles the messages the lobby asked for.
func (l *lobby) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		l.width, l.height = msg.Width, msg.Height
		return l, nil

	case resumeMsg:
		// A child just closed: the world may have changed behind us.
		if l.busy {
			return l, nil
		}
		return l, l.list()

	case tea.KeyPressMsg:
		return l, l.key(msg)

	case tickMsg:
		if !l.busy {
			return l, nil
		}
		l.frame++
		return l, tick()

	case listMsg:
		l.busy = false
		if msg.err != nil {
			return l, reportErr(msg.err)
		}
		l.items = msg.items
		l.clampItem()
		return l, nil

	case resultMsg:
		l.busy = false
		if msg.err != nil {
			return l, tea.Batch(reportErr(msg.err), l.list())
		}
		return l, tea.Batch(report("%s", msg.text), l.list())

	case detailMsg:
		if msg.err != nil {
			return l, reportErr(msg.err)
		}
		return l, push(newDetail(msg.detail))

	case settingsMsg:
		if msg.err != nil {
			return l, reportErr(msg.err)
		}
		return l, push(newSettingsForm(l.ctx, l.backend, msg.settings))

	case runActionMsg:
		return l, l.run(msg.kind, msg.name)
	}
	return l, nil
}

// key handles one keystroke of the menu.
func (l *lobby) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "q":
		return requestQuit()
	case "n":
		return push(newCreateForm(l.ctx, l.backend))
	case "up", "k":
		l.move(-1)
	case "down", "j":
		l.move(1)
	case "left", "h":
		if l.item > 0 {
			l.item--
		}
	case "right", "l":
		if l.item < len(l.items)-1 {
			l.item++
		}
	case "enter", " ":
		return l.activate()
	case "r":
		if l.busy {
			return nil
		}
		l.busy = true
		return tea.Batch(l.list(), tick())
	}
	return nil
}

// activate runs the selected menu entry.
func (l *lobby) activate() tea.Cmd {
	chosen := menuLines[l.cursor]

	switch chosen.action {
	case actQuit:
		return requestQuit()
	case actCreate:
		return push(newCreateForm(l.ctx, l.backend))
	}

	item, ok := l.current()
	if !ok {
		return report("no instance yet: press n to create one")
	}

	switch chosen.action {
	case actStart:
		return l.run(actStart, item.Name)
	case actStop:
		name := item.Name
		return askConfirm(
			"Stop "+name+"?",
			"The server saves the world and exits, waiting up to 20 seconds.",
			func() tea.Msg { return runActionMsg{kind: actStop, name: name} },
		)
	case actBackup:
		return push(newBackupForm(l.ctx, l.backend.Server, item))
	case actLogs:
		return push(newLogs(l.ctx, l.backend.Logs, item.Name))
	case actDetails:
		return l.show(item.Name)
	case actSettings:
		return l.config(item.Name)
	case actFiles:
		return push(newFiles(l.ctx, l.backend.Files, item))
	case actAdmins:
		return push(newAdmins(l.ctx, l.backend.Admins, item))
	case actBackups:
		return push(newBackups(l.ctx, l.backend.Server, item))
	case actService:
		return push(newService(l.ctx, l.backend.Service, item))
	case actDoctor:
		return push(newDoctor(l.backend.Env))
	case actPanel:
		return push(newPanel(l.ctx, l.backend.Panel, l.items))
	}
	return nil
}

// run starts one blocking server action away from the UI loop.
func (l *lobby) run(kind action, name string) tea.Cmd {
	backend, ctx := l.backend.Server, l.ctx
	l.busy, l.frame = true, 0

	verb := map[action]string{actStart: "starting", actStop: "stopping"}[kind]
	return tea.Batch(
		report("%s %s…", verb, name),
		func() tea.Msg {
			var text string
			var err error
			switch kind {
			case actStart:
				text, err = backend.Start(ctx, name)
			case actStop:
				text, err = backend.Stop(ctx, name)
			}
			return resultMsg{text: text, err: err}
		},
		tick(),
	)
}

// show loads the full configuration of one instance.
func (l *lobby) show(name string) tea.Cmd {
	backend := l.backend.Instances
	return func() tea.Msg {
		detail, err := backend.Show(name)
		return detailMsg{detail: detail, err: err}
	}
}

// config loads the editable settings of one instance.
func (l *lobby) config(name string) tea.Cmd {
	backend := l.backend.Instances
	return func() tea.Msg {
		settings, err := backend.Config(name)
		return settingsMsg{settings: settings, err: err}
	}
}

// list reloads the instance list without blocking the UI.
func (l *lobby) list() tea.Cmd {
	backend := l.backend.Instances
	return func() tea.Msg {
		items, err := backend.List()
		return listMsg{items: items, err: err}
	}
}

// move walks the menu skipping section headers and clamps at both ends.
func (l *lobby) move(delta int) {
	for i := l.cursor + delta; i >= 0 && i < len(menuLines); i += delta {
		if !menuLines[i].header {
			l.cursor = i
			return
		}
	}
}

// firstSelectable is the index of the first real entry.
func (l *lobby) firstSelectable() int {
	for i, line := range menuLines {
		if !line.header {
			return i
		}
	}
	return 0
}

// clampItem keeps the instance selection inside the list.
func (l *lobby) clampItem() {
	if len(l.items) == 0 {
		l.item = 0
		return
	}
	l.item = min(max(0, l.item), len(l.items)-1)
}

// current returns the selected instance, if any.
func (l *lobby) current() (Item, bool) {
	if l.item < 0 || l.item >= len(l.items) {
		return Item{}, false
	}
	return l.items[l.item], true
}

// View draws the banner, the selected instance, the menu and the keys.
func (l *lobby) View(width, height int) string {
	parts := []string{}

	// The block letters are only useful when they fit: a wrapped wordmark is
	// worse than the plain name.
	banner := wordmark()
	roomy := height <= 0 || height >= 36
	if roomy && (width <= 0 || textWidth(banner) <= width) {
		parts = append(parts,
			accentStyle.Render(banner),
			dimStyle.Render("dedicated game servers without memorising flags"),
		)
	} else {
		parts = append(parts,
			accentStyle.Render("GAMEHOST"),
			dimStyle.Render("dedicated game servers without memorising flags"),
		)
	}

	parts = append(parts, l.panel(width), l.menu(height), dimStyle.Render(l.footer()))
	return strings.Join(parts, "\n\n")
}

// panel draws the state of the selected instance.
func (l *lobby) panel(width int) string {
	item, ok := l.current()
	if !ok {
		return box("No instances yet.\n\nPress n, or choose New instance, to create one.", width)
	}

	phase := item.Phase
	if phase == "" {
		phase = "unknown"
	}

	marker, style := "○", dimStyle
	if item.Phase == "running" || item.Phase == "starting" {
		marker, style = "●", okStyle
	}

	head := titleStyle.Render(item.Name) + "   " + dimStyle.Render(item.Game)
	if len(l.items) > 1 {
		head += dimStyle.Render(fmt.Sprintf("   %d/%d", l.item+1, len(l.items)))
	}
	if l.busy {
		head += "   " + spinner[l.frame%len(spinner)]
	}

	state := style.Render(marker + " " + phase)
	if item.PID > 0 {
		state += dimStyle.Render(fmt.Sprintf("   pid %d", item.PID))
	}
	if up := uptime(item); up != "" {
		state += dimStyle.Render("   up " + up)
	}

	body := head
	if !l.busy {
		body += "\n" + state
	}
	if item.JoinLabel != "" || item.JoinValue != "" {
		body += "\n" + dimStyle.Render(item.JoinLabel+" ") + item.JoinValue
	}
	return box(body, width)
}

// menu draws the action list with a window that follows the cursor.
func (l *lobby) menu(height int) string {
	rows := l.menuRows(height)
	start := l.offset

	if l.cursor < start {
		start = l.cursor
	}
	if l.cursor >= start+rows {
		start = l.cursor - rows + 1
	}
	if start+rows > len(menuLines) {
		start = max(0, len(menuLines)-rows)
	}
	start = max(0, start)
	l.offset = start

	end := min(start+rows, len(menuLines))
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		line := menuLines[i]
		switch {
		case line.header:
			lines = append(lines, dimStyle.Render(strings.ToUpper(line.label)))
		case i == l.cursor:
			lines = append(lines, selectedStyle.Render("› "+line.label)+"  "+dimStyle.Render(line.hint))
		default:
			lines = append(lines, "  "+line.label)
		}
	}
	if start > 0 {
		lines = append([]string{dimStyle.Render("  ↑ more")}, lines...)
	}
	if end < len(menuLines) {
		lines = append(lines, dimStyle.Render("  ↓ more"))
	}
	return strings.Join(lines, "\n")
}

// menuRows is how many menu lines fit under the panel.
func (l *lobby) menuRows(height int) int {
	if height <= 0 {
		return len(menuLines)
	}
	banner := 7 // six block rows plus the subtitle
	if height < 36 {
		banner = 2
	}
	// gaps between banner, panel, menu and footer, plus the panel and footer.
	const fixed = 3 + 5 + 1
	return max(6, height-banner-fixed)
}

// footer is the key line of the lobby.
func (l *lobby) footer() string {
	if len(l.items) > 1 {
		return "↑↓ move    ←→ instance    ↵ select    n new    r refresh    ? help    q quit"
	}
	return "↑↓ move    ↵ select    n new    r refresh    ? help    q quit"
}

// Keys describes the lobby shortcuts for the help overlay.
func (l *lobby) Keys() []KeyHelp {
	return []KeyHelp{
		{"n", "create another instance", "gamehost create"},
		{"r", "reload the instance list", "gamehost list"},
		{"↑ ↓", "move in the menu", ""},
		{"← →", "switch instance", ""},
	}
}

// box wraps content in the instance frame, sized to the terminal.
func box(content string, width int) string {
	style := panelStyle
	if width > 12 {
		style = style.Width(min(70, width-6))
	}
	return style.Render(content)
}
