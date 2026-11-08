// Package tui renders the interactive lobby of gamehost.
//
// The package is presentation only: it imports nothing from the rest of the
// project. Everything it can do arrives through a Backend of function-valued
// groups, so the command layer stays the single owner of the behaviour and a
// screen can be tested with only the group it uses.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// DefaultLogLines is how much of the newest log the lobby shows at once.
const DefaultLogLines = 100

// Event is one step of a long operation.
//
// A backend that streams returns a channel of events and closes it when it is
// done. The final event carries Done, and Err when the operation failed.
type Event struct {
	Line string
	Err  error
	Done bool
}

// Item is one instance, already resolved for display.
type Item struct {
	Name      string
	Game      string
	Phase     string
	PID       int
	StartedAt time.Time
	JoinLabel string
	JoinValue string
	// BackupEvery is the schedule interval, empty when it is off.
	BackupEvery string
	BackupKeep  int
}

// Detail is the full configuration of one instance.
type Detail struct {
	Item
	Rows [][2]string
	Path string
}

// Settings is the editable view of one instance.
//
// Every value is a string so a form can hold it and the adapter can report the
// same error the related command prints. Parsing belongs to the adapter.
type Settings struct {
	Game            string
	Name            string
	WorldName       string
	Seed            string
	Mode            string
	MaxPlayers      string
	Port            string
	UDPPort         string
	Password        string
	AdminPassword   string
	Memory          string
	AutosaveMinutes string
	GameID          string
	Network         string
	ClusterToken    string
	Caves           string
	BackupEvery     string
	BackupKeep      string
}

// Archive is one backup file of an instance.
type Archive struct {
	Name     string
	Bytes    int64
	Modified time.Time
}

// Admin is one entry of the server's admin list.
type Admin struct {
	SteamID    string
	Name       string
	Privileges string
}

// Check is one doctor probe.
type Check struct {
	Name   string
	Status string
	Detail string
}

// Report is the doctor result.
type Report struct {
	Root    string
	Healthy bool
	Checks  []Check
}

// ServiceStatus is the systemd state of one instance.
type ServiceStatus struct {
	Unit      string
	Timer     string
	UnitDir   string
	Installed bool
	Active    bool
}

// GameChoice is one selectable game.
type GameChoice struct {
	Name  string
	Label string
}

// Backend is what the lobby needs from the command layer.
//
// Every function blocks except the streaming ones, which return a channel, so
// the model calls them from a command goroutine and the screen keeps drawing.
type Backend struct {
	Instances InstanceBackend
	Server    ServerBackend
	Logs      LogBackend
	Files     FileBackend
	Admins    AdminBackend
	Mods      ModsBackend
	Service   ServiceBackend
	Env       EnvBackend
	Panel     PanelBackend
}

// InstanceBackend manages instance configuration.
type InstanceBackend struct {
	List   func() ([]Item, error)
	Show   func(name string) (Detail, error)
	Create func(settings Settings) (Detail, error)
	Config func(name string) (Settings, error)
	Update func(name string, change Settings) (Detail, error)
}

// ServerBackend runs the lifecycle and the data operations of an instance.
type ServerBackend struct {
	Start       func(ctx context.Context, name string) (string, error)
	Stop        func(ctx context.Context, name string) (string, error)
	Backup      func(ctx context.Context, name string, keep int) (string, error)
	Restore     func(ctx context.Context, name string, archive string) (string, error)
	ImportWorld func(ctx context.Context, name, path string) (string, error)
	Backups     func(name string) ([]Archive, error)
	// Configure saves the backup schedule. The running supervisor reloads it
	// every tick, so it needs no restart.
	Configure func(name, every string, keep int) (string, error)
}

// LogBackend reads and follows the log of an instance.
type LogBackend struct {
	Follow func(ctx context.Context, name string, lines int) <-chan Event
}

// FileBackend downloads and updates the server files.
type FileBackend struct {
	Install func(ctx context.Context, name string) <-chan Event
	Update  func(ctx context.Context, name string) <-chan Event
}

// Mods is the mod configuration of one instance.
type Mods struct {
	Workshop []string
	Enabled  []string
}

// ModsBackend manages the mod list of a server that loads mods from a file.
type ModsBackend struct {
	List   func(name string) (Mods, error)
	Add    func(name, id string, workshop bool) (string, error)
	Remove func(name, id string, workshop bool) (string, error)
}

// AdminBackend manages the server's admin list.
type AdminBackend struct {
	List   func(name string) ([]Admin, error)
	Add    func(name, steamID, displayName string, limited bool) (string, error)
	Remove func(name, steamID string) (string, error)
}

// ServiceOptions are the advanced paths of `service install` and
// `service uninstall`. An empty value means the command's default.
type ServiceOptions struct {
	UnitDir   string
	Binary    string
	Systemctl string
}

// ServiceBackend manages the systemd units of an instance.
type ServiceBackend struct {
	Status    func(name string) (ServiceStatus, error)
	Install   func(name string, enable bool, opts ServiceOptions) (string, error)
	Uninstall func(name string, opts ServiceOptions) (string, error)
}

// EnvBackend reports the environment and the supported games.
type EnvBackend struct {
	Doctor  func() (Report, error)
	Games   func() []GameChoice
	Version func() string
}

// PanelBackend runs the HTTP panel while the lobby is open.
type PanelBackend struct {
	// Start listens on addr, starts the named instance when start is not
	// empty, and returns the address it bound plus a stop function that shuts
	// the listener down. Start is the only function that returns a cleanup,
	// because the panel outlives the call.
	Start func(ctx context.Context, addr, start string) (address string, stop func(), err error)
}

// validate reports every callable the lobby needs and does not have.
func (b Backend) validate() error {
	var missing []string
	need := func(name string, present bool) {
		if !present {
			missing = append(missing, name)
		}
	}

	need("Instances.List", b.Instances.List != nil)
	need("Instances.Show", b.Instances.Show != nil)
	need("Instances.Create", b.Instances.Create != nil)
	need("Instances.Config", b.Instances.Config != nil)
	need("Instances.Update", b.Instances.Update != nil)
	need("Server.Start", b.Server.Start != nil)
	need("Server.Stop", b.Server.Stop != nil)
	need("Server.Backup", b.Server.Backup != nil)
	need("Server.Restore", b.Server.Restore != nil)
	need("Server.ImportWorld", b.Server.ImportWorld != nil)
	need("Server.Backups", b.Server.Backups != nil)
	need("Logs.Follow", b.Logs.Follow != nil)
	need("Files.Install", b.Files.Install != nil)
	need("Files.Update", b.Files.Update != nil)
	need("Admins.List", b.Admins.List != nil)
	need("Admins.Add", b.Admins.Add != nil)
	need("Admins.Remove", b.Admins.Remove != nil)
	need("Mods.List", b.Mods.List != nil)
	need("Mods.Add", b.Mods.Add != nil)
	need("Mods.Remove", b.Mods.Remove != nil)
	need("Service.Status", b.Service.Status != nil)
	need("Service.Install", b.Service.Install != nil)
	need("Service.Uninstall", b.Service.Uninstall != nil)
	need("Env.Doctor", b.Env.Doctor != nil)
	need("Env.Games", b.Env.Games != nil)
	need("Env.Version", b.Env.Version != nil)
	need("Panel.Start", b.Panel.Start != nil)

	if len(missing) > 0 {
		return fmt.Errorf("tui: backend is missing %s", strings.Join(missing, ", "))
	}
	return nil
}

// Options configures one lobby run.
type Options struct {
	In      io.Reader
	Out     io.Writer
	Backend Backend
}

// Run renders the lobby until the user quits or ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	if opts.In == nil || opts.Out == nil {
		return errors.New("tui: input and output are required")
	}
	if err := opts.Backend.validate(); err != nil {
		return err
	}

	program := tea.NewProgram(newModel(ctx, opts.Backend),
		tea.WithContext(ctx),
		tea.WithInput(opts.In),
		tea.WithOutput(opts.Out),
	)

	// A cancelled context and Ctrl+C are a normal way to leave a lobby.
	if _, err := program.Run(); err != nil &&
		!errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return fmt.Errorf("run the lobby: %w", err)
	}
	return nil
}
