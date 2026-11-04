package corekeeper

import (
	"slices"
	"testing"
)

// covers the launch contract encoded from the reference container image
func TestInstallArgs(t *testing.T) {
	t.Parallel()

	want := []string{
		"+@sSteamCmdForcePlatformType", "linux",
		"+@sSteamCmdForcePlatformBitness", "64",
		"+force_install_dir", "/srv/gamehost/files",
		"+login", "anonymous",
		"+app_update", "1963720", "validate",
		"+quit",
	}

	if got := InstallArgs("/srv/gamehost/files"); !slices.Equal(got, want) {
		t.Errorf("InstallArgs() = %v, want %v", got, want)
	}
}

// covers the binary location used to verify an install
func TestBinaryPath(t *testing.T) {
	t.Parallel()

	if got, want := BinaryPath("/srv"), "/srv/CoreKeeperServer"; got != want {
		t.Errorf("BinaryPath() = %q, want %q", got, want)
	}
}
