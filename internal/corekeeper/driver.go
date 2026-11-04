package corekeeper

import (
	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
)

// Installer exposes the SteamCMD side of the package as a game.Installer.
type Installer struct{}

// AppID returns the Steam application of the dedicated server.
func (Installer) AppID() string { return AppID }

// InstallArgs returns the SteamCMD arguments that install the server into dir.
func (Installer) InstallArgs(dir string) []string { return InstallArgs(dir) }

// ServerPath returns the server executable inside an install directory.
func (Installer) ServerPath(installDir string) string { return BinaryPath(installDir) }

// Runner exposes the launch contract as a game.Runner.
type Runner struct{}

// Launch returns the command line that starts the server.
func (Runner) Launch(inst instance.Instance) (game.Launch, error) {
	args, err := Args(inst)
	if err != nil {
		return game.Launch{}, err
	}
	return game.Launch{
		Path: BinaryPath(inst.InstallDir),
		Args: args,
		Dir:  inst.InstallDir,
	}, nil
}

// SessionID returns the id the server wrote next to its binary.
func (Runner) SessionID(inst instance.Instance) string {
	id, err := ReadGameID(inst.InstallDir)
	if err != nil {
		return ""
	}
	return id
}

// Join reports the session id players type to reach the server.
func (r Runner) Join(inst instance.Instance) (string, string) {
	return "game id", r.SessionID(inst)
}
