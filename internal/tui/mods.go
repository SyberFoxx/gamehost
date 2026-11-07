package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

// modsMsg carries the mod list of one instance.
type modsMsg struct {
	list Mods
	err  error
}

// removeModMsg asks the screen to drop one id the user already confirmed.
type removeModMsg struct {
	id       string
	workshop bool
}

// mods lists workshop items and enabled mods of one instance.
type mods struct {
	ctx     context.Context
	backend ModsBackend
	item    Item

	list   Mods
	rows   []modRow
	cursor int
	err    string
	busy   bool
}

// modRow is one selectable line of the list.
type modRow struct {
	id       string
	workshop bool
}

// newMods returns the mod screen of one instance.
func newMods(ctx context.Context, backend ModsBackend, item Item) screen {
	return &mods{ctx: ctx, backend: backend, item: item}
}

// Init loads the list.
func (m *mods) Init() tea.Cmd { return m.load() }

// load reads the ini away from the UI loop.
func (m *mods) load() tea.Cmd {
	backend, name := m.backend, m.item.Name
	return func() tea.Msg {
		list, err := backend.List(name)
		return modsMsg{list: list, err: err}
	}
}

// Update folds the list and the action results.
func (m *mods) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m, m.key(msg)

	case resumeMsg:
		return m, m.load()

	case modsMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, reportErr(msg.err)
		}
		m.err = ""
		m.list = msg.list
		m.rows = m.rows[:0]
		for _, id := range m.list.Workshop {
			m.rows = append(m.rows, modRow{id: id, workshop: true})
		}
		for _, id := range m.list.Enabled {
			m.rows = append(m.rows, modRow{id: id})
		}
		m.clamp()
		return m, nil

	case resultMsg:
		m.busy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, reportErr(msg.err)
		}
		return m, tea.Batch(report("%s", msg.text), m.load())

	case removeModMsg:
		return m, m.runRemove(msg.id, msg.workshop)
	}
	return m, nil
}

// key opens a form, removes a row or moves.
func (m *mods) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		// An action in flight owns the screen: leaving now would drop its
		// answer on whatever is visible next.
		if m.busy {
			return nil
		}
		return pop()
	case "a", "n":
		return m.add(false)
	case "w":
		return m.add(true)
	case "r":
		if m.busy {
			return nil
		}
		m.busy = true
		return m.load()
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "d", "delete":
		return m.remove()
	}
	return nil
}

// add opens the form for a mod id or a workshop item.
func (m *mods) add(workshop bool) tea.Cmd {
	if m.busy {
		return nil
	}
	label, help, key := "Mod id", "the folder name the server loads, for example Hydrocraft", "mod"
	if workshop {
		label = "Workshop item"
		help = "the numeric id from the Steam Workshop URL"
		key = "workshop"
	}
	backend, name := m.backend, m.item.Name
	return push(&form{
		ctx:   m.ctx,
		title: "Add " + key + " · " + name,
		cli:   "gamehost mods add " + name,
		fields: []formField{
			textField("id", label, help, ""),
		},
		submit: func(values map[string]string) (string, error) {
			return backend.Add(name, values["id"], workshop)
		},
	})
}

// remove asks before dropping the selected id.
func (m *mods) remove() tea.Cmd {
	if m.busy || m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	row := m.rows[m.cursor]
	kind := "mod"
	if row.workshop {
		kind = "workshop item"
	}
	name := m.item.Name
	return askConfirm(
		"Remove "+kind+" "+row.id+"?",
		"The server must be stopped, and the mod will not load on "+name+".",
		func() tea.Msg { return removeModMsg{id: row.id, workshop: row.workshop} },
	)
}

// runRemove drops one id the user confirmed.
func (m *mods) runRemove(id string, workshop bool) tea.Cmd {
	if m.busy {
		return nil
	}
	m.busy = true
	backend, name := m.backend, m.item.Name
	return func() tea.Msg {
		text, err := backend.Remove(name, id, workshop)
		return resultMsg{text: text, err: err}
	}
}

// move walks the list.
func (m *mods) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = min(max(0, m.cursor+delta), len(m.rows)-1)
}

// clamp keeps the cursor inside a reloaded list.
func (m *mods) clamp() {
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	m.cursor = min(max(0, m.cursor), len(m.rows)-1)
}

// View draws the two sections.
func (m *mods) View(width, _ int) string {
	head := titleStyle.Render("Mods · "+m.item.Name) + "   " + dimStyle.Render("gamehost mods list "+m.item.Name)

	body := head + "\n\n"
	body += dimStyle.Render("workshop items") + "\n"
	if len(m.list.Workshop) == 0 {
		body += dimStyle.Render("  (none)") + "\n"
	}
	for i, id := range m.list.Workshop {
		body += m.rowView(i, id) + "\n"
	}
	body += "\n" + dimStyle.Render("mods") + "\n"
	if len(m.list.Enabled) == 0 {
		body += dimStyle.Render("  (none)") + "\n"
	}
	offset := len(m.list.Workshop)
	for i, id := range m.list.Enabled {
		body += m.rowView(offset+i, id) + "\n"
	}

	if m.err != "" {
		body += "\n" + errorStyle.Render(m.err)
	}
	body += "\n" + m.footer()
	return clipBlock(box(body, width), width)
}

// rowView draws one id with its selection marker.
func (m *mods) rowView(index int, id string) string {
	if index == m.cursor {
		return selectedStyle.Render("› " + id)
	}
	return "  " + id
}

// footer is the key line.
func (m *mods) footer() string {
	if m.busy {
		return dimStyle.Render("working…")
	}
	return dimStyle.Render("↑↓ move    a add mod    w add workshop    d remove    r reload    esc back    ? help")
}

// Keys describes the mod shortcuts.
func (m *mods) Keys() []KeyHelp {
	return []KeyHelp{
		{"a", "add a mod id", "gamehost mods add " + m.item.Name},
		{"w", "add a workshop item", "gamehost mods add " + m.item.Name + " --workshop"},
		{"d", "remove the selected id", "gamehost mods remove " + m.item.Name},
		{"r", "reload the list", "gamehost mods list " + m.item.Name},
	}
}
