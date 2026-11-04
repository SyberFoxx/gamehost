// Package corekeeper adapts the Core Keeper dedicated server.
package corekeeper

import "path/filepath"

// AppID is the Steam application of the dedicated server.
const AppID = "1963720"

// Binary is the server executable inside the install directory.
const Binary = "CoreKeeperServer"

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

// BinaryPath returns the server executable inside an install directory.
func BinaryPath(installDir string) string {
	return filepath.Join(installDir, Binary)
}
