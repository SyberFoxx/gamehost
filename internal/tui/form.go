package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// fieldKind is how a form field is edited.
type fieldKind int

const (
	kindText fieldKind = iota
	kindChoice
	kindLabel
)

// Games the wizard knows how to configure. These are the driver names the
// backend reports, never a display label.
const (
	gameCoreKeeper         = "corekeeper"
	gameProjectZomboid     = "projectzomboid"
	gameDontStarveTogether = "dontstarvetogether"
)

// instanceModeNormal is the default world mode.
const instanceModeNormal = "normal"

// instanceNetworkOffline is the value the backend stores for a LAN cluster.
const instanceNetworkOffline = "offline"

// choice is one option of a choice field.
type choice struct {
	value string
	label string
}

// label returns the text shown for a choice, falling back to its value.
func (c choice) text() string {
	if c.label != "" {
		return c.label
	}
	return c.value
}

// formField is one row of a form.
type formField struct {
	key     string
	label   string
	help    string
	kind    fieldKind
	input   textinput.Model
	choices []choice
	current int
	static  string
	raw     string
	when    func(values map[string]string) bool
}

// textField builds a text input row.
func textField(key, label, help, value string) formField {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = ""
	input.CharLimit = 128
	input.SetValue(value)
	return formField{key: key, label: label, help: help, kind: kindText, input: input}
}

// choiceField builds a row that cycles through options with left and right.
func choiceField(key, label, help, value string, choices ...choice) formField {
	field := formField{key: key, label: label, help: help, kind: kindChoice, choices: choices}
	for i, option := range choices {
		if option.value == value {
			field.current = i
		}
	}
	return field
}

// labelField shows a value the form cannot change. The display text and the
// value differ when the value is a driver name.
func labelField(key, label, display, value string) formField {
	return formField{key: key, label: label, kind: kindLabel, static: display, raw: value}
}

// visible reports whether the field applies to the current values.
func (f formField) visible(values map[string]string) bool {
	return f.when == nil || f.when(values)
}

// value returns the current value of the field.
func (f formField) value() string {
	switch f.kind {
	case kindChoice:
		if len(f.choices) == 0 {
			return ""
		}
		return f.choices[f.current].value
	case kindLabel:
		if f.raw != "" {
			return f.raw
		}
		return f.static
	default:
		return f.input.Value()
	}
}

// notGame shows a field for every game except one.
func notGame(game string) func(map[string]string) bool {
	return func(values map[string]string) bool { return values["game"] != game }
}

// whenGame builds a visibility rule for one game.
func whenGame(game string) func(map[string]string) bool {
	return func(values map[string]string) bool { return values["game"] == game }
}

// whenOnlineCluster shows the token only for a cluster that registers with
// Klei; an offline cluster never reads it.
func whenOnlineCluster(values map[string]string) bool {
	return values["game"] == gameDontStarveTogether && values["network"] != instanceNetworkOffline
}

// networkChoices are the two ways a Don't Starve Together cluster can listen.
var networkChoices = []choice{
	{"online", "online (Klei account, needs a token)"},
	{"offline", "offline (LAN only)"},
}

// cavesChoices control the second shard of a Don't Starve Together cluster.
var cavesChoices = []choice{
	{"yes", "yes (surface + caves)"},
	{"no", "no (surface only)"},
}

// setWhen attaches a visibility rule to every named field.
func setWhen(fields []formField, rules map[string]func(map[string]string) bool) {
	for i := range fields {
		if rule, ok := rules[fields[i].key]; ok {
			fields[i].when = rule
		}
	}
}

// formDoneMsg reports the result of submitting a form.
type formDoneMsg struct {
	note string
	err  error
}

// form is a generic screen that edits fields and submits their values.
type form struct {
	ctx    context.Context
	title  string
	cli    string
	fields []formField
	cursor int
	err    string
	busy   bool

	submit func(values map[string]string) (string, error)
}

// Init focuses the first editable field.
func (f *form) Init() tea.Cmd {
	f.cursor = f.firstSelectable()
	return f.focus()
}

// Update edits fields and handles the submit result.
func (f *form) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case formDoneMsg:
		f.busy = false
		if msg.err != nil {
			f.err = msg.err.Error()
			return f, nil
		}
		return f, tea.Batch(pop(), report("%s", msg.note))

	case tea.KeyPressMsg:
		return f, f.key(msg)

	case tea.PasteMsg:
		if f.cursorField().kind == kindText && f.cursorField().input.Focused() {
			var cmd tea.Cmd
			f.fields[f.cursor].input, cmd = f.fields[f.cursor].input.Update(msg)
			return f, cmd
		}
	}
	return f, nil
}

// key moves between fields, changes values or submits.
func (f *form) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return pop()
	case "up", "shift+tab":
		return f.move(-1)
	case "down", "tab":
		return f.move(1)
	case "enter":
		return f.apply()
	case "left":
		if f.cursorField().kind == kindChoice {
			return f.adjust(-1)
		}
	case "right":
		if f.cursorField().kind == kindChoice {
			return f.adjust(1)
		}
	}

	// A text field keeps the horizontal arrows: moving inside a mistyped
	// password is the whole reason the input is an editor and not a buffer.
	if f.cursorField().kind == kindText {
		var cmd tea.Cmd
		f.fields[f.cursor].input, cmd = f.fields[f.cursor].input.Update(msg)
		return cmd
	}
	return nil
}

// cursorField returns the field under the cursor.
func (f *form) cursorField() formField {
	if f.cursor < 0 || f.cursor >= len(f.fields) {
		return formField{kind: kindLabel}
	}
	return f.fields[f.cursor]
}

// values reads every field, including the hidden ones, so a value survives a
// game change and comes back when the field is shown again.
func (f *form) values() map[string]string {
	values := make(map[string]string, len(f.fields))
	for _, field := range f.fields {
		values[field.key] = field.value()
	}
	return values
}

// selectable lists the field indices the cursor may land on.
func (f *form) selectable() []int {
	values := f.values()
	indices := make([]int, 0, len(f.fields))
	for i, field := range f.fields {
		if field.kind == kindLabel || !field.visible(values) {
			continue
		}
		indices = append(indices, i)
	}
	return indices
}

// firstSelectable returns the index of the first editable field.
func (f *form) firstSelectable() int {
	if indices := f.selectable(); len(indices) > 0 {
		return indices[0]
	}
	return 0
}

// move walks the editable fields without leaving any hidden one focused.
func (f *form) move(delta int) tea.Cmd {
	indices := f.selectable()
	if len(indices) == 0 {
		return nil
	}

	position := 0
	for i, index := range indices {
		if index == f.cursor {
			position = i
		}
	}
	position = (position + delta + len(indices)) % len(indices)
	f.cursor = indices[position]
	return f.focus()
}

// focus gives the keyboard to the field under the cursor and takes it from the
// rest, so a stray keystroke never edits a hidden input.
func (f *form) focus() tea.Cmd {
	var cmds []tea.Cmd
	for i := range f.fields {
		if i == f.cursor && f.fields[i].kind == kindText {
			cmds = append(cmds, f.fields[i].input.Focus())
			continue
		}
		f.fields[i].input.Blur()
	}
	return tea.Batch(cmds...)
}

// adjust changes the value of a choice field.
func (f *form) adjust(delta int) tea.Cmd {
	field := &f.fields[f.cursor]
	if field.kind != kindChoice || len(field.choices) == 0 {
		return nil
	}
	field.current = (field.current + delta + len(field.choices)) % len(field.choices)

	// Changing the game hides and shows whole sections, so the cursor may now
	// point at a field that no longer exists.
	f.reclamp()
	return nil
}

// reclamp moves the cursor to a visible field after a value change.
func (f *form) reclamp() {
	indices := f.selectable()
	for _, index := range indices {
		if index == f.cursor {
			return
		}
	}
	f.cursor = f.firstSelectable()
}

// apply validates nothing itself: the adapter owns the rules and the messages.
func (f *form) apply() tea.Cmd {
	if f.busy {
		return nil
	}
	f.err = ""
	f.busy = true

	values := f.values()
	submit := f.submit
	return func() tea.Msg {
		note, err := submit(values)
		return formDoneMsg{note: note, err: err}
	}
}

// View draws the fields aligned, with the help of the focused row.
func (f *form) View(width, _ int) string {
	values := f.values()

	// The text inputs need their width while editing too, not only while
	// drawing, or a long value scrolls against a zero-width viewport.
	inputWidth := max(8, width-24)
	for i := range f.fields {
		if f.fields[i].kind == kindText {
			f.fields[i].input.SetWidth(inputWidth)
		}
	}

	labelWidth := 0
	for _, field := range f.fields {
		if field.visible(values) {
			labelWidth = max(labelWidth, textWidth(field.label))
		}
	}

	lines := make([]string, 0, len(f.fields)*2)
	for i, field := range f.fields {
		if !field.visible(values) {
			continue
		}

		label := dimStyle.Render(padRight(field.label, labelWidth))
		value := f.fieldView(field, width)
		line := label + "  " + value
		if i == f.cursor {
			line = selectedStyle.Render("›") + " " + label + "  " + value
		} else {
			line = "  " + label + "  " + value
		}
		lines = append(lines, line)

		if i == f.cursor && field.help != "" {
			lines = append(lines, "     "+dimStyle.Render(field.help))
		}
	}

	head := titleStyle.Render(f.title)
	if f.cli != "" {
		head += "   " + dimStyle.Render(f.cli)
	}
	body := head + "\n\n" + strings.Join(lines, "\n")

	if f.err != "" {
		body += "\n\n" + errorStyle.Render(f.err)
	}
	body += "\n\n" + dimStyle.Render(f.footer())

	return clipBlock(box(body, width), width)
}

// fieldView renders the editable half of one row.
func (f *form) fieldView(field formField, _ int) string {
	switch field.kind {
	case kindText:
		return field.input.View()
	case kindChoice:
		if len(field.choices) == 0 {
			return dimStyle.Render("(none)")
		}
		return dimStyle.Render("‹ ") + field.choices[field.current].text() + dimStyle.Render(" ›")
	case kindLabel:
		return dimStyle.Render(field.static)
	}
	return ""
}

// footer is the key line of a form.
func (f *form) footer() string {
	if f.busy {
		return "saving…"
	}
	return "tab next    ←→ change    enter save    esc cancel    ? help"
}

// Keys describes the form shortcuts for the help overlay.
func (f *form) Keys() []KeyHelp {
	return []KeyHelp{
		{"tab", "next field", ""},
		{"← →", "change a choice or toggle", ""},
		{"enter", "save", f.cli},
		{"esc", "cancel", ""},
	}
}

// editingText tells the root that ? belongs to the focused input.
func (f *form) editingText() bool { return true }

// settingsFrom reads the shared form values into the adapter settings type.
func settingsFrom(values map[string]string) Settings {
	return Settings{
		Game:            values["game"],
		Name:            values["name"],
		WorldName:       values["world"],
		Seed:            values["seed"],
		Mode:            values["mode"],
		MaxPlayers:      values["players"],
		Port:            values["port"],
		UDPPort:         values["udp"],
		Password:        values["password"],
		AdminPassword:   values["admin-password"],
		Memory:          values["memory"],
		AutosaveMinutes: values["autosave"],
		GameID:          values["game-id"],
		Network:         values["network"],
		ClusterToken:    values["cluster-token"],
		Caves:           values["caves"],
		BackupEvery:     values["backup-every"],
		BackupKeep:      values["backup-keep"],
	}
}

// modeChoices are the world modes the server accepts.
var modeChoices = []choice{
	{"normal", "normal"},
	{"hard", "hard"},
	{"creative", "creative"},
	{"casual", "casual"},
}

// gameLabel resolves the display name of one game.
func gameLabel(backend Backend, name string) string {
	for _, game := range backend.Env.Games() {
		if game.Name == name {
			return game.Label
		}
	}
	return name
}

// newCreateForm returns the wizard that writes a new instance.
func newCreateForm(ctx context.Context, backend Backend) screen {
	gameOptions := make([]choice, 0, 2)
	for _, game := range backend.Env.Games() {
		gameOptions = append(gameOptions, choice{value: game.Name, label: game.Label})
	}
	if len(gameOptions) == 0 {
		gameOptions = []choice{{value: "corekeeper", label: "Core Keeper"}}
	}

	fields := []formField{
		textField("name", "Name", "letters, digits, - or _; becomes the directory", ""),
		choiceField("game", "Game", "the driver stays the same for the life of the instance", gameOptions[0].value, gameOptions...),
		textField("world", "World name", "shown to players; empty uses the instance name", ""),
		textField("seed", "Seed", "empty means random", ""),
		choiceField("mode", "Mode", "", instanceModeNormal, modeChoices...),
		textField("players", "Max players", "1-8 (Core Keeper), 1-64 (Don't Starve Together)", "8"),
		choiceField("network", "Cluster", "online registers with Klei; offline is LAN only", "online", networkChoices...),
		textField("port", "Port", "empty uses the default: 10999 for Don't Starve Together", ""),
		textField("cluster-token", "Cluster token", "from accounts.klei.com, online clusters only", ""),
		choiceField("caves", "Caves shard", "the second shard takes the next UDP port", "no", cavesChoices...),
		textField("udp", "Direct port", "Project Zomboid direct-connect port", ""),
		textField("password", "Join password", "empty means anyone with the address can join", ""),
		textField("admin-password", "Admin password", "required on the first start", ""),
		textField("memory", "Memory", "JVM heap such as 4g; 8g is the server default", defaultProjectZomboidMemory),
		textField("autosave", "Autosave", "minutes between world saves; empty keeps the game default", ""),
		textField("game-id", "Game ID", "empty generates one on the next start", ""),
		textField("backup-every", "Backup every", "for example 24h; empty is off", ""),
		textField("backup-keep", "Backup keep", "archives to keep", "5"),
	}

	setWhen(fields, map[string]func(map[string]string) bool{
		"seed":           whenGame(gameCoreKeeper),
		"mode":           whenGame(gameCoreKeeper),
		"players":        notGame(gameProjectZomboid),
		"password":       whenGame(gameCoreKeeper),
		"game-id":        whenGame(gameCoreKeeper),
		"network":        whenGame(gameDontStarveTogether),
		"cluster-token":  whenOnlineCluster,
		"caves":          whenGame(gameDontStarveTogether),
		"udp":            whenGame(gameProjectZomboid),
		"admin-password": whenGame(gameProjectZomboid),
		"memory":         whenGame(gameProjectZomboid),
		"autosave":       whenGame(gameProjectZomboid),
	})

	return &form{
		ctx:    ctx,
		title:  "New instance",
		cli:    "gamehost create",
		fields: fields,
		submit: func(values map[string]string) (string, error) {
			detail, err := backend.Instances.Create(settingsFrom(values))
			if err != nil {
				return "", err
			}
			return "created " + detail.Name, nil
		},
	}
}

// newSettingsForm returns the editor of an existing instance.
func newSettingsForm(ctx context.Context, backend Backend, settings Settings) screen {
	fields := []formField{
		labelField("game", "Game", gameLabel(backend, settings.Game), settings.Game),
		labelField("name", "Name", settings.Name, settings.Name),
		textField("world", "World name", "shown to players", settings.WorldName),
		textField("seed", "Seed", "empty means random", settings.Seed),
		choiceField("mode", "Mode", "", settings.Mode, modeChoices...),
		textField("players", "Max players", "1-8 (Core Keeper), 1-64 (Don't Starve Together)", settings.MaxPlayers),
		choiceField("network", "Cluster", "online registers with Klei; offline is LAN only", settings.Network, networkChoices...),
		textField("port", "Port", "empty uses the default: 10999 for Don't Starve Together", settings.Port),
		textField("cluster-token", "Cluster token", "from accounts.klei.com, online clusters only", settings.ClusterToken),
		choiceField("caves", "Caves shard", "the second shard takes the next UDP port", settings.Caves, cavesChoices...),
		textField("udp", "Direct port", "Project Zomboid direct-connect port", settings.UDPPort),
		textField("password", "Join password", "empty means anyone with the address can join", settings.Password),
		textField("admin-password", "Admin password", "required on the first start", settings.AdminPassword),
		textField("memory", "Memory", "JVM heap such as 4g; 8g is the server default", settings.Memory),
		textField("autosave", "Autosave", "minutes between world saves; empty keeps the game default", settings.AutosaveMinutes),
		textField("game-id", "Game ID", "empty generates one on the next start", settings.GameID),
		textField("backup-every", "Backup every", "for example 24h; empty is off", settings.BackupEvery),
		textField("backup-keep", "Backup keep", "archives to keep", settings.BackupKeep),
	}

	setWhen(fields, map[string]func(map[string]string) bool{
		"seed":           whenGame(gameCoreKeeper),
		"mode":           whenGame(gameCoreKeeper),
		"players":        notGame(gameProjectZomboid),
		"password":       whenGame(gameCoreKeeper),
		"game-id":        whenGame(gameCoreKeeper),
		"network":        whenGame(gameDontStarveTogether),
		"cluster-token":  whenOnlineCluster,
		"caves":          whenGame(gameDontStarveTogether),
		"udp":            whenGame(gameProjectZomboid),
		"admin-password": whenGame(gameProjectZomboid),
		"memory":         whenGame(gameProjectZomboid),
		"autosave":       whenGame(gameProjectZomboid),
	})

	return &form{
		ctx:    ctx,
		title:  "Settings · " + settings.Name,
		cli:    "gamehost config " + settings.Name,
		fields: fields,
		submit: func(values map[string]string) (string, error) {
			updated, err := backend.Instances.Update(settings.Name, settingsFrom(values))
			if err != nil {
				return "", err
			}
			return "saved " + updated.Name, nil
		},
	}
}
