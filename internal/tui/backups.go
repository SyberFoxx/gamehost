package tui

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// restoreMsg asks the screen to replace the world with one archive the user
// already confirmed.
type restoreMsg struct{ archive string }

// importMsg asks the screen to import one save the user already confirmed.
type importMsg struct{ path string }

// archivesMsg carries the backup list of one instance.
type archivesMsg struct {
	list []Archive
	err  error
}

// backups lists, creates, restores and imports the world archives.
type backups struct {
	ctx     context.Context
	backend ServerBackend
	item    Item

	list    []Archive
	cursor  int
	busy    bool
	err     string
	imports bool
	path    textinput.Model
}

// newBackups returns the backup screen of one instance.
func newBackups(ctx context.Context, backend ServerBackend, item Item) screen {
	path := textinput.New()
	path.Prompt = ""
	path.Placeholder = "/path/to/save.world.gzip"
	path.CharLimit = 256
	return &backups{ctx: ctx, backend: backend, item: item, path: path}
}

// Init loads the archives.
func (b *backups) Init() tea.Cmd { return b.load() }

// load reads the backup directory away from the UI loop.
func (b *backups) load() tea.Cmd {
	backend, name := b.backend, b.item.Name
	return func() tea.Msg {
		list, err := backend.Backups(name)
		return archivesMsg{list: list, err: err}
	}
}

// Update folds the list, the import input and the result of an action.
func (b *backups) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.path.SetWidth(max(12, msg.Width-18))
		return b, nil

	case tea.KeyPressMsg:
		return b, b.key(msg)

	case tea.PasteMsg:
		if !b.imports {
			return b, nil
		}
		var cmd tea.Cmd
		b.path, cmd = b.path.Update(msg)
		return b, cmd

	case resumeMsg:
		return b, b.load()

	case archivesMsg:
		b.busy = false
		if msg.err != nil {
			b.err = msg.err.Error()
			return b, reportErr(msg.err)
		}
		b.err = ""
		b.list = msg.list
		b.clamp()
		return b, nil

	case resultMsg:
		b.busy = false
		if msg.err != nil {
			b.err = msg.err.Error()
			return b, reportErr(msg.err)
		}
		b.imports = false
		return b, tea.Batch(report("%s", msg.text), b.load())

	case restoreMsg:
		return b, b.runRestore(msg.archive)

	case importMsg:
		b.imports = false
		b.path.Blur()
		return b, b.runImport(msg.path)
	}
	return b, nil
}

// key routes between the archive list and the import prompt.
func (b *backups) key(msg tea.KeyPressMsg) tea.Cmd {
	if b.imports {
		switch msg.String() {
		case "esc":
			b.imports = false
			b.path.Blur()
			return nil
		case "enter":
			return b.importWorld()
		}
		var cmd tea.Cmd
		b.path, cmd = b.path.Update(msg)
		return cmd
	}

	switch msg.String() {
	case "esc", "q":
		return pop()
	case "up", "k":
		b.move(-1)
	case "down", "j":
		b.move(1)
	case "r":
		if b.busy {
			return nil
		}
		b.busy = true
		return b.load()
	case "b":
		return b.backup()
	case "enter":
		return b.restore()
	case "i":
		if b.busy {
			return nil
		}
		b.imports = true
		return b.path.Focus()
	}
	return nil
}

// backup archives the world now; it is safe while the server is stopped.
func (b *backups) backup() tea.Cmd {
	if b.busy {
		return nil
	}
	b.busy = true
	backend, ctx, name := b.backend, b.ctx, b.item.Name
	return tea.Batch(
		report("backing up %s…", name),
		func() tea.Msg {
			text, err := backend.Backup(ctx, name)
			return resultMsg{text: text, err: err}
		},
	)
}

// restore asks before replacing the world with the selected archive.
func (b *backups) restore() tea.Cmd {
	archive, ok := b.current()
	if !ok {
		return report("no archive selected")
	}
	if b.busy {
		return nil
	}

	name := b.item.Name
	return askConfirm(
		"Restore "+name+"?",
		"The world is replaced with "+archive.Name+". The current data is archived first and the server must be stopped.",
		func() tea.Msg { return restoreMsg{archive: archive.Name} },
	)
}

// runRestore performs a restore the user confirmed.
func (b *backups) runRestore(archive string) tea.Cmd {
	if b.busy {
		return nil
	}
	b.busy = true
	backend, ctx, name := b.backend, b.ctx, b.item.Name
	return func() tea.Msg {
		text, err := backend.Restore(ctx, name, archive)
		return resultMsg{text: text, err: err}
	}
}

// importWorld asks before copying a save into the instance slot.
func (b *backups) importWorld() tea.Cmd {
	path := strings.TrimSpace(b.path.Value())
	if path == "" {
		b.err = "a world path is required"
		return nil
	}
	if b.busy {
		return nil
	}

	name := b.item.Name
	return askConfirm(
		"Import a world into "+name+"?",
		"The save at "+path+" replaces slot data. The current data is archived first and the server must be stopped.",
		func() tea.Msg { return importMsg{path: path} },
	)
}

// runImport performs an import the user confirmed.
func (b *backups) runImport(path string) tea.Cmd {
	if b.busy {
		return nil
	}
	b.busy = true
	backend, ctx, name := b.backend, b.ctx, b.item.Name
	return func() tea.Msg {
		text, err := backend.ImportWorld(ctx, name, path)
		return resultMsg{text: text, err: err}
	}
}

// move walks the archive list.
func (b *backups) move(delta int) {
	if len(b.list) == 0 {
		return
	}
	b.cursor = min(max(0, b.cursor+delta), len(b.list)-1)
}

// clamp keeps the cursor inside a reloaded list.
func (b *backups) clamp() {
	if len(b.list) == 0 {
		b.cursor = 0
		return
	}
	b.cursor = min(max(0, b.cursor), len(b.list)-1)
}

// current returns the selected archive, if any.
func (b *backups) current() (Archive, bool) {
	if b.cursor < 0 || b.cursor >= len(b.list) {
		return Archive{}, false
	}
	return b.list[b.cursor], true
}

// View draws the archive table or the import prompt.
func (b *backups) View(width, _ int) string {
	if b.imports {
		b.path.SetWidth(max(12, width-18))
	}
	head := titleStyle.Render("Backups · "+b.item.Name) + "   " + dimStyle.Render("gamehost backup "+b.item.Name)

	body := head + "\n\n"
	switch {
	case b.imports:
		body += titleStyle.Render("Import a world") + "\n\n" +
			dimStyle.Render("path  ") + b.path.View() + "\n\n" +
			dimStyle.Render("the file must end in .world.gzip and the server must be stopped")
	case len(b.list) == 0:
		body += dimStyle.Render("no archives yet: press b to make the first one")
	default:
		lines := make([]string, 0, len(b.list))
		for i, archive := range b.list {
			row := padRight(archive.Modified.Format("2006-01-02 15:04"), 18) +
				"  " + padRight(humanBytes(archive.Bytes), 10) + "  " + archive.Name
			if i == b.cursor {
				lines = append(lines, selectedStyle.Render("› "+clip(row, max(10, width-6))))
				continue
			}
			lines = append(lines, "  "+clip(row, max(10, width-6)))
		}
		body += strings.Join(lines, "\n")
	}

	if b.err != "" {
		body += "\n\n" + errorStyle.Render(b.err)
	}
	body += "\n\n" + b.footer()
	return clipBlock(box(body, width), width)
}

// footer is the key line of the backup screen.
func (b *backups) footer() string {
	if b.imports {
		return dimStyle.Render("enter import    esc cancel")
	}
	if b.busy {
		return dimStyle.Render("working…")
	}
	return dimStyle.Render("↑↓ move    enter restore    b backup    i import    r reload    esc back")
}

// editingText tells the root that ? belongs to the import path.
func (b *backups) editingText() bool { return b.imports }

// Keys describes the backup shortcuts.
func (b *backups) Keys() []KeyHelp {
	name := b.item.Name
	return []KeyHelp{
		{"b", "archive the world now", "gamehost backup " + name},
		{"enter", "restore the selected archive", "gamehost restore " + name},
		{"i", "import a .world.gzip", "gamehost import-world " + name},
		{"r", "reload the archive list", ""},
	}
}
