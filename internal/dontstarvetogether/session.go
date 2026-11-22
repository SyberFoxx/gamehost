package dontstarvetogether

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// sessionIDPattern matches the line the Master shard prints once it has a
// world session, for example:
// Telling Client our new session identifier: D8835938996F494F
var sessionIDPattern = regexp.MustCompile(`Telling Client our new session identifier:\s*([0-9A-Fa-f]+)\s*$`)

// ServerLogPath returns the Master shard log, where the server prints the
// session identifier. The Caves shard has no such line of its own.
func ServerLogPath(inst instance.Instance) string {
	return filepath.Join(ClusterDir(inst), Shard, "server_log.txt")
}

// SessionID returns the identifier of the current world session. The server
// writes a new one on every start, so the log is the only source of truth;
// before the first start there is no log and the value stays empty.
func (Runner) SessionID(inst instance.Instance) string {
	id, err := readSessionID(ServerLogPath(inst))
	if err != nil {
		return ""
	}
	return id
}

// Join reports the session identifier players can use to find the world,
// falling back to the browser name before the first start.
func (r Runner) Join(inst instance.Instance) (string, string) {
	if id := r.SessionID(inst); id != "" {
		return "session id", id
	}
	return "cluster", inst.World.Name
}

// readSessionID returns the last matching identifier in the log, because a
// restart appends a new line without clearing the old ones.
func readSessionID(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	// A log line is short, but a corrupted or verbose line must not abort a
	// status read; give the scanner room beyond its 64 KiB default.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var last string
	for scanner.Scan() {
		if match := sessionIDPattern.FindStringSubmatch(scanner.Text()); match != nil {
			last = match[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan %q: %w", path, err)
	}
	return last, nil
}
