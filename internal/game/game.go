// Package game describes the game-specific parts of an install and a launch.
package game

import "github.com/syberfoxx/gamehost/internal/instance"

// Launch is one server command line.
type Launch struct {
	Path string
	Args []string
	Dir  string
}

// Installer downloads and locates the server files of one game.
type Installer interface {
	AppID() string
	InstallArgs(dir string) []string
	ServerPath(installDir string) string
}

// Runner turns an instance into a launch and reports how players join it.
type Runner interface {
	Launch(inst instance.Instance) (Launch, error)
	SessionID(inst instance.Instance) string
	Join(inst instance.Instance) (label, value string)
}

// Registration pairs a game with its driver.
type Registration struct {
	Game      string
	Installer Installer
	Runner    Runner
}
