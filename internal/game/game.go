// Package game describes the game-specific parts of an install and a launch.
package game

import "github.com/syberfoxx/gamehost/internal/instance"

// Launch is one server command line.
type Launch struct {
	Path string
	Args []string
	Dir  string
	// Extra lists additional processes that belong to the same launch, such
	// as a second game shard. They join the primary process group, so one
	// stop signal reaches every one of them.
	Extra []Launch
}

// Installer downloads and locates the server files of one game.
type Installer interface {
	AppID() string
	InstallArgs(dir string) []string
	ServerPath(installDir string) string
}

// Preparer writes the configuration files a server reads at startup.
// The launch calls it before the process starts, so a driver that keeps its
// settings in files of its own owns the write instead of the CLI.
type Preparer interface {
	Prepare(inst instance.Instance) error
}

// Validator checks the settings a launch would apply without writing a file.
// A command that changes a running instance uses it to refuse an impossible
// value before it stops the server.
type Validator interface {
	Validate(inst instance.Instance) error
}

// Runner turns an instance into a launch and reports how players join it.
type Runner interface {
	Launch(inst instance.Instance) (Launch, error)
	SessionID(inst instance.Instance) string
	Join(inst instance.Instance) (label, value string)
}

// Admin is one entry of a server admin list, in the shape the terminal shows.
type Admin struct {
	ID         string
	Name       string
	Privileges string
}

// AdminStore reads and writes the admin list a server owns. Core Keeper keeps
// a JSON file with Steam IDs and privileges; Don't Starve Together keeps one
// Klei ID per line. The CLI and the lobby only see this interface.
type AdminStore interface {
	Admins(inst instance.Instance) ([]Admin, error)
	AddAdmin(inst instance.Instance, id, name string, limited bool) (bool, error)
	RemoveAdmin(inst instance.Instance, id string) error
}

// Mods is the mod configuration a server reads at startup.
// Workshop lists Steam Workshop ids; Enabled lists the mod ids to load.
type Mods struct {
	Workshop []string
	Enabled  []string
}

// ModStore reads and writes the mod list a server owns. Only games whose
// server reads mods from a file implement it.
type ModStore interface {
	Mods(inst instance.Instance) (Mods, error)
	SaveMods(inst instance.Instance, mods Mods) error
}

// Registration pairs a game with its driver.
type Registration struct {
	Game      string
	Label     string // display name for people, never a command-line value
	Installer Installer
	Runner    Runner
}
