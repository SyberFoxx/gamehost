// Package projectzomboid adapts the Project Zomboid dedicated server.
package projectzomboid

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
)

const (
	// AppID is the Steam application of the dedicated server.
	AppID = "380870"
	// Script is the launcher that ships inside the install directory.
	Script = "start-server.sh"
)

// InstallArgs returns the SteamCMD arguments that install the server into dir.
func InstallArgs(dir string) []string {
	return []string{
		"+@sSteamCmdForcePlatformType", "linux",
		"+@sSteamCmdForcePlatformBitness", "64",
		"+force_install_dir", dir,
		"+login", "anonymous",
		"+app_update", AppID, "validate",
		"+quit",
	}
}

// ServerPath returns the launch script inside an install directory.
func ServerPath(installDir string) string {
	return filepath.Join(installDir, Script)
}

// Args returns the launch arguments for an instance.
func Args(inst instance.Instance) []string {
	args := []string{
		"-servername", inst.World.Name,
		"-port", strconv.Itoa(inst.Network.Port),
		"-udpport", strconv.Itoa(inst.Network.UDPPort),
		"-cachedir=" + inst.DataDir,
	}
	if inst.AdminPassword != "" {
		args = append(args, "-adminpassword", inst.AdminPassword)
	}
	return args
}

// Installer exposes the SteamCMD side as a game.Installer.
type Installer struct{}

// AppID returns the Steam application of the dedicated server.
func (Installer) AppID() string { return AppID }

// InstallArgs returns the SteamCMD arguments that install the server into dir.
func (Installer) InstallArgs(dir string) []string { return InstallArgs(dir) }

// ServerPath returns the launch script inside an install directory.
func (Installer) ServerPath(installDir string) string { return ServerPath(installDir) }

// Runner exposes the launch contract as a game.Runner.
type Runner struct{}

// Launch returns the command line that starts the server.
func (Runner) Launch(inst instance.Instance) (game.Launch, error) {
	return game.Launch{
		Path: ServerPath(inst.InstallDir),
		Args: Args(inst),
		Dir:  inst.InstallDir,
	}, nil
}

// SessionID is empty: Project Zomboid has no join code.
func (Runner) SessionID(instance.Instance) string { return "" }

// Join reports the port players connect to.
func (Runner) Join(inst instance.Instance) (string, string) {
	return "join", fmt.Sprintf("port %d UDP", inst.Network.Port)
}
