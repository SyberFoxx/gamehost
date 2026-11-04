// Package home resolves the gamehost data directory.
package home

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnvRoot overrides the data directory when set.
const EnvRoot = "GAMEHOST_HOME"

// Dir returns the root directory for all mutable gamehost state.
func Dir() (string, error) {
	if dir := os.Getenv(EnvRoot); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", EnvRoot, err)
		}
		return abs, nil
	}

	// The XDG spec requires XDG_DATA_HOME to be absolute; ignore it otherwise.
	if dir := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "gamehost"), nil
	}

	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(userHome, ".local", "share", "gamehost"), nil
}
