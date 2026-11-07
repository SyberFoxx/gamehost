package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// press builds a key message for the shortcuts the lobby uses.
func press(text string) tea.KeyPressMsg {
	switch text {
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "delete":
		return tea.KeyPressMsg{Code: tea.KeyDelete}
	case " ":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	default:
		r := []rune(text)[0]
		return tea.KeyPressMsg{Code: r, Text: text}
	}
}

// step applies one message and returns the model plus its command.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	model, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want tui.Model", next)
	}
	return model, cmd
}

// drain runs a command and folds a batch into the messages it produced.
func drain(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, inner := range batch {
			out = append(out, drain(t, inner)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// find returns the first message of the requested type.
func find[T tea.Msg](t *testing.T, msgs []tea.Msg) T {
	t.Helper()
	for _, msg := range msgs {
		if found, ok := msg.(T); ok {
			return found
		}
	}
	var zero T
	t.Fatalf("no %T in %#v", zero, msgs)
	return zero
}

// fake is a Backend that records what the lobby asked of it.
type fake struct {
	items    []Item
	detail   Detail
	settings Settings
	archives []Archive
	admins   []Admin
	report   Report
	service  ServiceStatus
	games    []GameChoice

	follow      <-chan Event
	followAt    int
	backupKeep  int
	serviceOpts ServiceOptions
	version     string

	panelAddr string
	panelStop func()
	panelBoot string

	calls  []string
	err    error
	onStop func()
}

func (f *fake) record(call string) {
	f.calls = append(f.calls, call)
}

func (f *fake) backend() Backend {
	return Backend{
		Instances: InstanceBackend{
			List: func() ([]Item, error) {
				f.record("list")
				return f.items, f.err
			},
			Show: func(name string) (Detail, error) {
				f.record("show " + name)
				return f.detail, f.err
			},
			Create: func(settings Settings) (Detail, error) {
				f.record("create " + settings.Name)
				detail := f.detail
				if detail.Name == "" {
					detail.Name = settings.Name
				}
				return detail, f.err
			},
			Config: func(name string) (Settings, error) {
				f.record("config " + name)
				return f.settings, f.err
			},
			Update: func(name string, _ Settings) (Detail, error) {
				f.record("update " + name)
				detail := f.detail
				if detail.Name == "" {
					detail.Name = name
				}
				return detail, f.err
			},
		},
		Server: ServerBackend{
			Start: func(_ context.Context, name string) (string, error) {
				f.record("start " + name)
				return "started " + name, f.err
			},
			Stop: func(_ context.Context, name string) (string, error) {
				f.record("stop " + name)
				return "stopped " + name, f.err
			},
			Backup: func(_ context.Context, name string, keep int) (string, error) {
				f.backupKeep = keep
				f.record("backup " + name)
				return "backed up " + name, f.err
			},
			Restore: func(_ context.Context, name, archive string) (string, error) {
				f.record("restore " + name + " " + archive)
				return "restored " + name, f.err
			},
			ImportWorld: func(_ context.Context, name, path string) (string, error) {
				f.record("import " + name + " " + path)
				return "imported " + name, f.err
			},
			Backups: func(name string) ([]Archive, error) {
				f.record("backups " + name)
				return f.archives, f.err
			},
		},
		Logs: LogBackend{
			Follow: func(_ context.Context, name string, lines int) <-chan Event {
				f.followAt = lines
				f.record("follow " + name)
				return f.follow
			},
		},
		Files: FileBackend{
			Install: func(_ context.Context, name string) <-chan Event {
				f.record("install " + name)
				return closedStream(Event{Line: "downloading", Done: true})
			},
			Update: func(_ context.Context, name string) <-chan Event {
				f.record("update " + name)
				return closedStream(Event{Line: "updating", Done: true})
			},
		},
		Mods: ModsBackend{
			List: func(name string) (Mods, error) {
				f.record("mods " + name)
				return Mods{}, f.err
			},
			Add: func(name, id string, workshop bool) (string, error) {
				f.record("mod add " + name + " " + id)
				return "added", f.err
			},
			Remove: func(name, id string, workshop bool) (string, error) {
				f.record("mod remove " + name + " " + id)
				return "removed", f.err
			},
		},
		Admins: AdminBackend{
			List: func(name string) ([]Admin, error) {
				f.record("admins " + name)
				return f.admins, f.err
			},
			Add: func(name, steamID, _ string, limited bool) (string, error) {
				f.record("admin add " + name + " " + steamID)
				if limited {
					return "limited", nil
				}
				return "added", nil
			},
			Remove: func(name, steamID string) (string, error) {
				f.record("admin remove " + name + " " + steamID)
				return "removed", f.err
			},
		},
		Service: ServiceBackend{
			Status: func(name string) (ServiceStatus, error) {
				f.record("service status " + name)
				return f.service, f.err
			},
			Install: func(name string, _ bool, opts ServiceOptions) (string, error) {
				f.serviceOpts = opts
				f.record("service install " + name)
				return "installed", f.err
			},
			Uninstall: func(name string, opts ServiceOptions) (string, error) {
				f.serviceOpts = opts
				f.record("service uninstall " + name)
				return "uninstalled", f.err
			},
		},
		Env: EnvBackend{
			Doctor: func() (Report, error) {
				f.record("doctor")
				return f.report, f.err
			},
			Games:   func() []GameChoice { return f.games },
			Version: func() string { return f.version },
		},
		Panel: PanelBackend{
			Start: func(_ context.Context, _, start string) (string, func(), error) {
				f.panelBoot = start
				f.record("panel")
				return f.panelAddr, f.panelStop, f.err
			},
		},
	}
}

// closedStream returns a channel that already holds events and is closed.
func closedStream(events ...Event) <-chan Event {
	ch := make(chan Event, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch
}

// newTestModel returns a root model showing one screen at a fixed size.
func newTestModel(t *testing.T, f *fake, top screen) Model {
	t.Helper()
	m := newModel(context.Background(), f.backend())
	if top != nil {
		m.stack = []screen{top}
	}
	m.width, m.height = 100, 40
	return m
}

func menuIndex(t *testing.T, want action) int {
	t.Helper()
	for i, line := range menuLines {
		if !line.header && line.action == want {
			return i
		}
	}
	t.Fatalf("menu has no entry for action %d", want)
	return 0
}

func TestBackendValidation(t *testing.T) {
	t.Parallel()
	if err := (Backend{}).validate(); err == nil {
		t.Fatalf("an empty backend should be refused")
	}
	if err := (&fake{}).backend().validate(); err != nil {
		t.Fatalf("a complete backend should pass: %v", err)
	}
}

func TestLobbyLoadsInstances(t *testing.T) {
	t.Parallel()
	f := &fake{items: []Item{{Name: "Partidita"}, {Name: "Otro"}}}
	m := newTestModel(t, f, nil)

	msgs := drain(t, m.Init())
	loaded := find[listMsg](t, msgs)
	if loaded.err != nil {
		t.Fatalf("list: %v", loaded.err)
	}

	m, _ = step(t, m, loaded)
	lobby := m.stack[0].(*lobby)
	if len(lobby.items) != 2 {
		t.Fatalf("got %d items, want 2", len(lobby.items))
	}
}

func TestMenuCursorSkipsHeaders(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, &fake{}, nil)
	lobby := m.stack[0].(*lobby)

	for range menuLines {
		m, _ = step(t, m, press("down"))
		if menuLines[lobby.cursor].header {
			t.Fatalf("cursor landed on the header %q", menuLines[lobby.cursor].label)
		}
	}
	if lobby.cursor != menuIndex(t, actQuit) {
		t.Fatalf("cursor = %d, want the last entry", lobby.cursor)
	}

	for range menuLines {
		m, _ = step(t, m, press("up"))
	}
	if lobby.cursor != menuIndex(t, actStart) {
		t.Fatalf("cursor = %d, want the first entry", lobby.cursor)
	}
}

func TestStopAsksBeforeRunning(t *testing.T) {
	t.Parallel()
	f := &fake{items: []Item{{Name: "Partidita"}}}
	m := newTestModel(t, f, nil)
	m, _ = step(t, m, listMsg{items: f.items})
	m.stack[0].(*lobby).cursor = menuIndex(t, actStop)

	m, cmd := step(t, m, press("enter"))
	question := find[confirmMsg](t, drain(t, cmd))
	if !strings.Contains(question.question, "Stop Partidita") {
		t.Fatalf("question = %q", question.question)
	}
	if strings.Contains(question.detail, "steam") {
		t.Fatalf("detail = %q", question.detail)
	}

	m, _ = step(t, m, question)
	if m.confirm == nil {
		t.Fatalf("the confirmation was not opened")
	}
	if m.stack[0].(*lobby).busy {
		t.Fatalf("the lobby should not be busy before the answer")
	}

	_, cmd = step(t, m, press("y"))
	run := find[runActionMsg](t, drain(t, cmd))
	if run.kind != actStop || run.name != "Partidita" {
		t.Fatalf("runActionMsg = %#v", run)
	}
}

func TestCreateFormHidesFieldsPerGame(t *testing.T) {
	t.Parallel()
	f := &fake{games: []GameChoice{
		{Name: gameCoreKeeper, Label: "Core Keeper"},
		{Name: gameProjectZomboid, Label: "Project Zomboid"},
		{Name: gameDontStarveTogether, Label: "Don't Starve Together"},
	}}
	form := newCreateForm(context.Background(), f.backend()).(*form)

	visible := func() map[string]bool {
		values := form.values()
		out := make(map[string]bool, len(form.fields))
		for _, field := range form.fields {
			out[field.key] = field.visible(values)
		}
		return out
	}

	if !visible()["seed"] || visible()["udp"] || visible()["caves"] {
		t.Fatalf("Core Keeper fields are wrong: %v", visible())
	}

	// Move to the game choice and switch to Project Zomboid.
	form.cursor = indexOf(t, form.fields, "game")
	form.adjust(1)

	if visible()["seed"] || !visible()["udp"] || !visible()["admin-password"] {
		t.Fatalf("Project Zomboid fields are wrong: %v", visible())
	}
	if form.values()["game"] != gameProjectZomboid {
		t.Fatalf("game value = %q", form.values()["game"])
	}

	// Project Zomboid is the second game; once more reaches Don't Starve Together.
	form.adjust(1)
	if form.values()["game"] != gameDontStarveTogether {
		t.Fatalf("game value = %q", form.values()["game"])
	}
	if visible()["seed"] || visible()["password"] || visible()["udp"] || !visible()["network"] || !visible()["caves"] {
		t.Fatalf("Don't Starve Together fields are wrong: %v", visible())
	}
	if !visible()["cluster-token"] {
		t.Fatalf("an online cluster must ask for its token: %v", visible())
	}

	// The token is only read by an online cluster, so the field disappears
	// when the choice moves to offline instead of staying visible and unused.
	form.cursor = indexOf(t, form.fields, "network")
	form.adjust(1)
	if form.values()["network"] != instanceNetworkOffline {
		t.Fatalf("network value = %q", form.values()["network"])
	}
	if visible()["cluster-token"] {
		t.Fatalf("an offline cluster still shows the token field: %v", visible())
	}
}

// covers the horizontal arrows inside a text field: they move the input
// cursor instead of being eaten by the choice handling
func TestFormKeepsArrowsInsideTextFields(t *testing.T) {
	t.Parallel()
	f := &fake{games: []GameChoice{{Name: gameCoreKeeper, Label: "Core Keeper"}}}
	form := newCreateForm(context.Background(), f.backend()).(*form)

	name := indexOf(t, form.fields, "name")
	form.Init()
	form.fields[name].input.SetValue("abc")

	form.Update(press("left"))
	if form.cursor != name {
		t.Fatalf("left moved the form cursor to %d, want it to stay on the field", form.cursor)
	}
	if got := form.fields[name].input.Position(); got != 2 {
		t.Fatalf("input position = %d, want 2 after one left arrow", got)
	}
}

// indexOf returns the field index of a key.
func indexOf(t *testing.T, fields []formField, key string) int {
	t.Helper()
	for i, field := range fields {
		if field.key == key {
			return i
		}
	}
	t.Fatalf("no field %q", key)
	return 0
}

func TestCreateFormSubmitReturnsTheCreatedName(t *testing.T) {
	t.Parallel()
	f := &fake{games: []GameChoice{{Name: gameCoreKeeper, Label: "Core Keeper"}}}
	form := newCreateForm(context.Background(), f.backend()).(*form)
	form.fields[indexOf(t, form.fields, "name")].input.SetValue("partidita")

	msgs := drain(t, form.apply())
	done := find[formDoneMsg](t, msgs)
	if done.err != nil {
		t.Fatalf("create: %v", done.err)
	}
	if done.note != "created partidita" {
		t.Fatalf("note = %q", done.note)
	}
	if len(f.calls) != 1 || f.calls[0] != "create partidita" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestFormRefusesAFailedSubmitWithoutLeaving(t *testing.T) {
	t.Parallel()
	f := &fake{games: []GameChoice{{Name: gameCoreKeeper, Label: "Core Keeper"}}, err: errors.New("name is taken")}
	form := newCreateForm(context.Background(), f.backend()).(*form)
	form.fields[indexOf(t, form.fields, "name")].input.SetValue("partidita")

	msgs := drain(t, form.apply())
	done := find[formDoneMsg](t, msgs)
	if done.err == nil {
		t.Fatalf("a failed create should report an error")
	}

	_, cmd := form.Update(done)
	if cmd != nil {
		t.Fatalf("a failed submit must not leave the form")
	}
	if !strings.Contains(form.err, "name is taken") {
		t.Fatalf("form error = %q", form.err)
	}
	if form.busy {
		t.Fatalf("the form should be editable again")
	}
}

func TestQuestionMarkDoesNotOpenHelpWhileTyping(t *testing.T) {
	t.Parallel()
	f := &fake{games: []GameChoice{{Name: gameCoreKeeper, Label: "Core Keeper"}}}
	m := newTestModel(t, f, newCreateForm(context.Background(), f.backend()))

	m, _ = step(t, m, press("?"))
	if m.help {
		t.Fatalf("? opened the help overlay inside a text field")
	}
}

func TestQuestionMarkOpensHelpOutsideAForm(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, &fake{}, nil)

	m, _ = step(t, m, press("?"))
	if !m.help {
		t.Fatalf("? did not open the help overlay")
	}
	if !strings.Contains(m.render(), "Keys") {
		t.Fatalf("the help overlay is empty")
	}
}

func TestMenuEntryPushesTheExpectedScreen(t *testing.T) {
	t.Parallel()
	f := &fake{items: []Item{{Name: "Partidita"}}}
	m := newTestModel(t, f, nil)
	m, _ = step(t, m, listMsg{items: f.items})

	tests := []struct {
		action action
		check  func(t *testing.T, s screen)
	}{
		{actDetails, func(t *testing.T, s screen) {
			_, ok := s.(*detail)
			if !ok {
				t.Fatalf("got %T", s)
			}
		}},
		{actLogs, func(t *testing.T, s screen) {
			_, ok := s.(*logs)
			if !ok {
				t.Fatalf("got %T", s)
			}
		}},
		{actAdmins, func(t *testing.T, s screen) {
			_, ok := s.(*admins)
			if !ok {
				t.Fatalf("got %T", s)
			}
		}},
		{actBackups, func(t *testing.T, s screen) {
			_, ok := s.(*backups)
			if !ok {
				t.Fatalf("got %T", s)
			}
		}},
		{actService, func(t *testing.T, s screen) {
			_, ok := s.(*service)
			if !ok {
				t.Fatalf("got %T", s)
			}
		}},
		{actDoctor, func(t *testing.T, s screen) {
			_, ok := s.(*doctor)
			if !ok {
				t.Fatalf("got %T", s)
			}
		}},
		{actPanel, func(t *testing.T, s screen) {
			_, ok := s.(*panel)
			if !ok {
				t.Fatalf("got %T", s)
			}
		}},
		{actFiles, func(t *testing.T, s screen) {
			_, ok := s.(*files)
			if !ok {
				t.Fatalf("got %T", s)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(menuLines[menuIndex(t, tt.action)].label, func(t *testing.T) {
			m := newTestModel(t, f, nil)
			m, _ = step(t, m, listMsg{items: f.items})
			m.stack[0].(*lobby).cursor = menuIndex(t, tt.action)

			m, cmd := step(t, m, press("enter"))
			push, ok := settle(t, &m, cmd).(pushMsg)
			if !ok {
				t.Fatalf("no screen was pushed")
			}
			m, _ = step(t, m, push)
			if len(m.stack) != 2 {
				t.Fatalf("stack has %d screens, want 2", len(m.stack))
			}
			tt.check(t, m.stack[len(m.stack)-1])
		})
	}
}

// settle runs commands until one of them asks for a new screen, feeding the
// messages a screen needs in between.
func settle(t *testing.T, m *Model, cmd tea.Cmd) tea.Msg {
	t.Helper()
	for i := 0; i < 4 && cmd != nil; i++ {
		next := cmd
		cmd = nil
		for _, msg := range drain(t, next) {
			if push, ok := msg.(pushMsg); ok {
				return push
			}
			*m, cmd = step(t, *m, msg)
		}
	}
	t.Fatalf("no pushMsg after settling")
	return nil
}

func TestLogsAppendAndFollow(t *testing.T) {
	t.Parallel()
	ch := make(chan Event, 4)
	f := &fake{follow: ch}
	logs := newLogs(context.Background(), f.backend().Logs, "Partidita").(*logs)

	cmd := logs.Init()
	ch <- Event{Line: "boot"}
	ch <- Event{Line: "ready"}

	logs, cmd = applyTo(t, logs, cmd)
	logs, cmd = applyTo(t, logs, cmd)
	if len(logs.lines) != 2 || logs.lines[0] != "boot" {
		t.Fatalf("lines = %q", logs.lines)
	}

	ch <- Event{Done: true}
	logs, _ = applyTo(t, logs, cmd)
	if !logs.done {
		t.Fatalf("the log did not finish")
	}
	if !logs.follow {
		t.Fatalf("following should stay on until the user scrolls")
	}
}

// applyTo runs one command and feeds its message back into a screen of the
// same concrete type.
func applyTo[S screen](t *testing.T, s S, cmd tea.Cmd) (S, tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatalf("no command to apply")
	}
	next, nextCmd := s.Update(cmd())
	typed, ok := next.(S)
	if !ok {
		t.Fatalf("Update returned %T, want %T", next, s)
	}
	return typed, nextCmd
}

func TestLogsEscCancelsTheFollower(t *testing.T) {
	t.Parallel()
	ch := make(chan Event, 1)
	f := &fake{follow: ch}
	logs := newLogs(context.Background(), f.backend().Logs, "Partidita").(*logs)
	logs.Init()

	if logs.cancel == nil {
		t.Fatalf("the follower has no cancel function")
	}
	_, cmd := logs.Update(press("esc"))
	if logs.cancel != nil {
		t.Fatalf("esc left the follower running")
	}
	if _, ok := drain(t, cmd)[0].(popMsg); !ok {
		t.Fatalf("esc did not leave the screen")
	}
}

func TestBackupsRestoreAsksAndNamesTheArchive(t *testing.T) {
	t.Parallel()
	f := &fake{
		archives: []Archive{{Name: "2026-11-05_10-00-00.tar.gz", Bytes: 1024, Modified: time.Now()}},
	}
	backups := newBackups(context.Background(), f.backend().Server, Item{Name: "Partidita"}).(*backups)
	backups.list = f.archives

	_, cmd := backups.Update(press("enter"))
	question := find[confirmMsg](t, drain(t, cmd))
	if !strings.Contains(question.question, "Restore Partidita") {
		t.Fatalf("question = %q", question.question)
	}
	if !strings.Contains(question.detail, f.archives[0].Name) {
		t.Fatalf("detail = %q", question.detail)
	}
}

func TestBackupsImportRequiresAPath(t *testing.T) {
	t.Parallel()
	f := &fake{}
	backups := newBackups(context.Background(), f.backend().Server, Item{Name: "Partidita"}).(*backups)

	_, cmd := backups.Update(press("i"))
	if cmd == nil {
		t.Fatalf("i did not focus the path input")
	}
	_, cmd = backups.Update(press("enter"))
	if cmd != nil {
		t.Fatalf("an empty path should not ask to import")
	}
	if !strings.Contains(backups.err, "path is required") {
		t.Fatalf("err = %q", backups.err)
	}
}

func TestCancellingAConfirmationDoesNotWedgeTheScreen(t *testing.T) {
	t.Parallel()
	f := &fake{archives: []Archive{{Name: "2026-11-05_10-00-00.tar.gz", Modified: time.Now()}}}
	backups := newBackups(context.Background(), f.backend().Server, Item{Name: "Partidita"}).(*backups)
	backups.list = f.archives
	m := newTestModel(t, f, backups)

	m, cmd := step(t, m, press("enter"))
	m, _ = step(t, m, find[confirmMsg](t, drain(t, cmd)))
	if m.confirm == nil {
		t.Fatalf("restore did not ask for confirmation")
	}

	m, _ = step(t, m, press("n"))
	if m.confirm != nil {
		t.Fatalf("the confirmation did not close")
	}
	if backups.busy {
		t.Fatalf("cancelling left the screen busy")
	}
}

func TestWindowSizeReachesAPushedScreen(t *testing.T) {
	t.Parallel()
	f := &fake{follow: make(chan Event)}
	logs := newLogs(context.Background(), f.backend().Logs, "Partidita").(*logs)
	m := newTestModel(t, f, logs)

	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	if logs.width != 120 || logs.height != 30 {
		t.Fatalf("log screen size = %dx%d, want 120x30", logs.width, logs.height)
	}
}

func TestPanelStartsAndStops(t *testing.T) {
	t.Parallel()
	stopped := false
	f := &fake{
		panelAddr: "127.0.0.1:8080",
		panelStop: func() { stopped = true },
	}
	p := newPanel(context.Background(), f.backend().Panel, nil).(*panel)

	cmd := p.start()
	p, _ = applyTo(t, p, cmd)
	if p.stop == nil || p.address != "127.0.0.1:8080" {
		t.Fatalf("panel = %q, stop nil = %v", p.address, p.stop == nil)
	}

	_, cmd = p.Update(press("esc"))
	if !stopped {
		t.Fatalf("leaving the panel did not stop the listener")
	}
	if _, ok := drain(t, cmd)[0].(popMsg); !ok {
		t.Fatalf("esc did not leave the panel")
	}
}

// covers the tail editor: reloading the follower with the size typed
func TestLogsTailEditorReloadsTheFollower(t *testing.T) {
	t.Parallel()
	follow := make(chan Event, 1)
	follow <- Event{Done: true}
	f := &fake{follow: follow}
	l := newLogs(context.Background(), f.backend().Logs, "Partidita").(*logs)

	l.Init()
	if f.followAt != DefaultLogLines {
		t.Fatalf("initial tail = %d, want %d", f.followAt, DefaultLogLines)
	}

	l.Update(press("n"))
	if !l.editing {
		t.Fatalf("n did not open the tail editor")
	}
	l.input.SetValue("250")

	_, cmd := l.Update(press("enter"))
	drain(t, cmd)

	if l.editing {
		t.Fatalf("enter left the tail editor open")
	}
	if l.tail != 250 {
		t.Fatalf("tail = %d, want 250", l.tail)
	}
	if f.followAt != 250 {
		t.Fatalf("the follower restarted with %d lines, want 250", f.followAt)
	}
}

// covers the retention the backup form hands to the backend
func TestBackupFormPassesTheRetention(t *testing.T) {
	t.Parallel()
	f := &fake{}
	form := newBackupForm(context.Background(), f.backend().Server, Item{Name: "Partidita", BackupKeep: 3}).(*form)

	keep := indexOf(t, form.fields, "keep")
	if got := form.fields[keep].input.Value(); got != "3" {
		t.Fatalf("default keep = %q, want the instance value 3", got)
	}
	form.fields[keep].input.SetValue("2")

	done := find[formDoneMsg](t, drain(t, form.apply()))
	if done.err != nil {
		t.Fatalf("backup: %v", done.err)
	}
	if f.backupKeep != 2 {
		t.Fatalf("backend got keep %d, want 2", f.backupKeep)
	}
}

// covers the systemd options reaching the install and uninstall calls
func TestServiceOptionsReachTheBackend(t *testing.T) {
	t.Parallel()
	f := &fake{}
	s := newService(context.Background(), f.backend().Service, Item{Name: "Partidita"}).(*service)
	s.opts = ServiceOptions{UnitDir: "/tmp/units", Binary: "/tmp/gamehost", Systemctl: "/tmp/systemctl"}

	drain(t, s.install(true))
	if f.serviceOpts != s.opts {
		t.Fatalf("install options = %#v, want %#v", f.serviceOpts, s.opts)
	}

	drain(t, s.runUninstall())
	if f.serviceOpts != s.opts {
		t.Fatalf("uninstall options = %#v, want %#v", f.serviceOpts, s.opts)
	}
}

// covers the panel starting an instance, the lobby version of serve --start
func TestPanelBootStartReachesTheBackend(t *testing.T) {
	t.Parallel()
	f := &fake{panelAddr: "127.0.0.1:8080"}
	p := newPanel(context.Background(), f.backend().Panel, []Item{{Name: "Partidita"}}).(*panel)

	p.Update(press("b"))
	if p.bootName() != "Partidita" {
		t.Fatalf("boot = %q, want Partidita", p.bootName())
	}

	p, _ = applyTo(t, p, p.start())
	if f.panelBoot != "Partidita" {
		t.Fatalf("backend started %q with the panel, want Partidita", f.panelBoot)
	}
}

// covers the build version the environment screen shows
func TestDoctorShowsTheVersion(t *testing.T) {
	t.Parallel()
	f := &fake{version: "v9.9.9"}
	d := newDoctor(f.backend().Env).(*doctor)
	d.Init()

	if !strings.Contains(d.View(100, 40), "v9.9.9") {
		t.Fatalf("the view does not show the version:\n%s", d.View(100, 40))
	}
}

func TestDoctorShowsTheVerdict(t *testing.T) {
	t.Parallel()
	f := &fake{report: Report{
		Root:    "/tmp/gamehost",
		Healthy: false,
		Checks: []Check{
			{Name: "platform", Status: "ok", Detail: "linux"},
			{Name: "steamcmd", Status: "fail", Detail: "not found"},
		},
	}}
	d := newDoctor(f.backend().Env).(*doctor)
	cmd := d.Init()
	d, _ = applyTo(t, d, cmd)

	view := d.View(120, 40)
	for _, want := range []string{"platform", "steamcmd", "unhealthy", "not found"} {
		if !strings.Contains(view, want) {
			t.Fatalf("doctor view is missing %q:\n%s", want, view)
		}
	}
}

// covers leaving the mod screen only when nothing is in flight
func TestModsKeepsTheScreenWhileBusy(t *testing.T) {
	t.Parallel()
	f := &fake{}
	m := newMods(context.Background(), f.backend().Mods, Item{Name: "corekeeper"}).(*mods)
	m.busy = true

	for _, key := range []string{"esc", "q"} {
		if cmd := m.key(press(key)); cmd != nil {
			t.Fatalf("%q left the mod screen while an action was in flight", key)
		}
	}

	m.busy = false
	cmd := m.key(press("esc"))
	if cmd == nil {
		t.Fatalf("esc stopped leaving the mod screen")
	}
	if _, ok := cmd().(popMsg); !ok {
		t.Fatalf("esc did not ask to pop the screen")
	}
}
