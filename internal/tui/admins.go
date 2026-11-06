package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// adminsMsg carries the admin list of one instance.
type adminsMsg struct {
	list []Admin
	err  error
}

// removeAdminMsg asks the screen to drop one admin the user already confirmed.
type removeAdminMsg struct{ steamID string }

// admins lists, adds and removes the admins of one instance.
type admins struct {
	ctx     context.Context
	backend AdminBackend
	item    Item

	list   []Admin
	cursor int
	err    string
	busy   bool
}

// newAdmins returns the admin screen of one instance.
func newAdmins(ctx context.Context, backend AdminBackend, item Item) screen {
	return &admins{ctx: ctx, backend: backend, item: item}
}

// Init loads the admin list.
func (a *admins) Init() tea.Cmd { return a.load() }

// load reads the list away from the UI loop.
func (a *admins) load() tea.Cmd {
	backend, name := a.backend, a.item.Name
	return func() tea.Msg {
		list, err := backend.List(name)
		return adminsMsg{list: list, err: err}
	}
}

// Update folds the list and the result of an action.
func (a *admins) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return a, a.key(msg)

	case resumeMsg:
		// The add form may have changed the file behind us.
		return a, a.load()

	case adminsMsg:
		a.busy = false
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, reportErr(msg.err)
		}
		a.err = ""
		a.list = msg.list
		a.clamp()
		return a, nil

	case resultMsg:
		a.busy = false
		if msg.err != nil {
			a.err = msg.err.Error()
			return a, reportErr(msg.err)
		}
		return a, tea.Batch(report("%s", msg.text), a.load())

	case removeAdminMsg:
		return a, a.runRemove(msg.steamID)
	}
	return a, nil
}

// key opens the add form or asks before removing.
func (a *admins) key(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		return pop()
	case "a", "n":
		if a.busy {
			return nil
		}
		return push(newAdminForm(a.ctx, a.backend, a.item.Name))
	case "r":
		if a.busy {
			return nil
		}
		a.busy = true
		return a.load()
	case "up", "k":
		a.move(-1)
	case "down", "j":
		a.move(1)
	case "d", "delete":
		return a.remove()
	}
	return nil
}

// remove asks before dropping the selected admin.
func (a *admins) remove() tea.Cmd {
	admin, ok := a.current()
	if !ok {
		return report("no admin selected")
	}
	if a.busy {
		return nil
	}

	name := a.item.Name
	return askConfirm(
		"Remove admin "+admin.SteamID+"?",
		"They lose every admin command on "+name+". The server must be stopped.",
		func() tea.Msg { return removeAdminMsg{steamID: admin.SteamID} },
	)
}

// runRemove drops an admin the user confirmed.
func (a *admins) runRemove(steamID string) tea.Cmd {
	if a.busy {
		return nil
	}
	a.busy = true
	backend, name := a.backend, a.item.Name
	return func() tea.Msg {
		text, err := backend.Remove(name, steamID)
		return resultMsg{text: text, err: err}
	}
}

// move walks the list.
func (a *admins) move(delta int) {
	if len(a.list) == 0 {
		return
	}
	a.cursor = min(max(0, a.cursor+delta), len(a.list)-1)
}

// clamp keeps the cursor inside a reloaded list.
func (a *admins) clamp() {
	if len(a.list) == 0 {
		a.cursor = 0
		return
	}
	a.cursor = min(max(0, a.cursor), len(a.list)-1)
}

// current returns the selected admin, if any.
func (a *admins) current() (Admin, bool) {
	if a.cursor < 0 || a.cursor >= len(a.list) {
		return Admin{}, false
	}
	return a.list[a.cursor], true
}

// View draws the admin table.
func (a *admins) View(width, _ int) string {
	head := titleStyle.Render("Admins · "+a.item.Name) + "   " + dimStyle.Render("gamehost admin list "+a.item.Name)

	body := head + "\n\n"
	if len(a.list) == 0 {
		body += dimStyle.Render("no admins yet: press a to add the first player")
	} else {
		lines := make([]string, 0, len(a.list))
		for i, admin := range a.list {
			row := padRight(admin.SteamID, 18) + "  " + padRight(admin.Privileges, 8) + "  " + admin.Name
			if i == a.cursor {
				lines = append(lines, selectedStyle.Render("› "+row))
				continue
			}
			lines = append(lines, "  "+row)
		}
		body += strings.Join(lines, "\n")
	}

	if a.err != "" {
		body += "\n\n" + errorStyle.Render(a.err)
	}
	body += "\n\n" + a.footer()
	return clipBlock(box(body, width), width)
}

// footer is the key line of the admin screen.
func (a *admins) footer() string {
	if a.busy {
		return dimStyle.Render("working…")
	}
	return dimStyle.Render("↑↓ move    a add    d remove    r reload    esc back    ? help")
}

// Keys describes the admin shortcuts.
func (a *admins) Keys() []KeyHelp {
	return []KeyHelp{
		{"a", "add an admin", "gamehost admin add " + a.item.Name},
		{"d", "remove the selected admin", "gamehost admin remove " + a.item.Name},
		{"r", "reload the list", "gamehost admin list " + a.item.Name},
	}
}

// newAdminForm returns the form that grants admin rights.
func newAdminForm(ctx context.Context, backend AdminBackend, name string) screen {
	return &form{
		ctx:   ctx,
		title: "Add admin · " + name,
		cli:   "gamehost admin add " + name,
		fields: []formField{
			textField("steam", "Player ID", "Steam ID (Core Keeper) or Klei ID such as KU_abc123 (Don't Starve Together)", ""),
			textField("display", "Display name", "optional, Core Keeper only", ""),
			choiceField("privileges", "Privileges", "limited is Core Keeper only; DST admins are always full", "full",
				choice{"full", "full"}, choice{"limited", "limited"}),
		},
		submit: func(values map[string]string) (string, error) {
			return backend.Add(name, values["steam"], values["display"], values["privileges"] == "limited")
		},
	}
}
