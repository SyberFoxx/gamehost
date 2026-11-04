// Package steamcmd locates and runs the SteamCMD client.
package steamcmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// EnvPath overrides the SteamCMD entry point when set.
const EnvPath = "GAMEHOST_STEAMCMD"

// ErrNotFound reports that no SteamCMD installation was found.
var ErrNotFound = errors.New("steamcmd not found")

// Locate returns the path to the SteamCMD entry point.
func Locate() (string, error) {
	homeDir, _ := os.UserHomeDir() // an empty home only skips the fallback candidates
	return locate(exec.LookPath, homeDir, os.Getenv)
}

// locate resolves SteamCMD from the environment, PATH and user-local installs.
func locate(look func(string) (string, error), homeDir string, getenv func(string) string) (string, error) {
	if path := getenv(EnvPath); path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("%s=%s: %w", EnvPath, path, err)
		}
		return path, nil
	}

	if path, err := look("steamcmd"); err == nil {
		return path, nil
	}

	if homeDir == "" {
		return "", ErrNotFound
	}

	candidates := []string{
		filepath.Join(homeDir, ".local", "share", "steamcmd", "steamcmd.sh"),
		filepath.Join(homeDir, "steamcmd", "steamcmd.sh"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", ErrNotFound
}
