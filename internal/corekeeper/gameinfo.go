package corekeeper

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GameInfoFile is written next to the binary at every session start.
const GameInfoFile = "GameInfo.txt"

// ReadGameID returns the current session id from GameInfo.txt.
func ReadGameID(installDir string) (string, error) {
	path := filepath.Join(installDir, GameInfoFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %q: %w", path, err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		id, found := strings.CutPrefix(strings.TrimSpace(line), "GameID:")
		if !found {
			continue
		}
		id = strings.TrimSpace(id)
		if id == "" {
			return "", fmt.Errorf("%s: empty game id", path)
		}
		return id, nil
	}
	return "", fmt.Errorf("%s: no game id line", path)
}
