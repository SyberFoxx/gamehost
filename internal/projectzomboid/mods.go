package projectzomboid

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/syberfoxx/gamehost/internal/atomicfile"
	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
)

// ErrInvalidMod reports a workshop id or mod id the server would not load.
var ErrInvalidMod = errors.New("invalid mod")

const maxModIDLen = 128

// IniPath returns the server ini of one instance.
func IniPath(inst instance.Instance) string {
	return filepath.Join(inst.DataDir, "Server", inst.World.Name+".ini")
}

// Mods reads WorkshopItems and Mods from the server ini.
func (Runner) Mods(inst instance.Instance) (game.Mods, error) {
	lines, err := readIni(IniPath(inst))
	if err != nil {
		return game.Mods{}, err
	}
	return game.Mods{
		Workshop: splitList(iniValue(lines, "WorkshopItems")),
		Enabled:  splitList(iniValue(lines, "Mods")),
	}, nil
}

// SaveMods replaces the two mod lines and leaves every other line untouched.
func (Runner) SaveMods(inst instance.Instance, mods game.Mods) error {
	workshop, err := normalizeIDs(mods.Workshop, ParseWorkshopID)
	if err != nil {
		return err
	}
	enabled, err := normalizeIDs(mods.Enabled, ParseModID)
	if err != nil {
		return err
	}

	path := IniPath(inst)
	lines, err := readIni(path)
	if err != nil {
		return err
	}
	lines = setIniValue(lines, "WorkshopItems", strings.Join(workshop, ";"))
	lines = setIniValue(lines, "Mods", strings.Join(enabled, ";"))

	data := []byte(strings.Join(lines, "\n"))
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if err := atomicfile.Replace(path, data, 0o644); err != nil {
		return fmt.Errorf("save %q: %w", path, err)
	}
	return nil
}

// ParseWorkshopID validates a Steam Workshop item id.
func ParseWorkshopID(value string) (string, error) {
	id := strings.TrimSpace(value)
	if id == "" || len(id) > 20 {
		return "", fmt.Errorf("workshop id %q must be a number: %w", value, ErrInvalidMod)
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("workshop id %q must be digits only: %w", value, ErrInvalidMod)
		}
	}
	return id, nil
}

// ParseModID validates one mod id, which is the folder name the server loads.
func ParseModID(value string) (string, error) {
	id := strings.TrimSpace(value)
	if id == "" || len(id) > maxModIDLen {
		return "", fmt.Errorf("mod id %q must be 1-%d characters: %w", value, maxModIDLen, ErrInvalidMod)
	}
	for _, r := range id {
		isLetter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		if !isLetter && !isDigit && r != '_' && r != '-' && r != '.' {
			return "", fmt.Errorf("mod id %q may only use letters, digits, _, - or .: %w", value, ErrInvalidMod)
		}
	}
	return id, nil
}

// readIni reads the server ini as lines. The server creates it on the first
// start, so a missing file is a clear error instead of an empty config.
func readIni(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("server ini %q does not exist yet: start the server once: %w", path, err)
		}
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"), nil
}

// iniValue returns the value of a top-level key, or an empty string.
func iniValue(lines []string, key string) string {
	prefix := key + "="
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

// setIniValue replaces a key in place, or appends it when the server did not
// write it. Everything else in the file stays byte for byte.
func setIniValue(lines []string, key, value string) []string {
	prefix := key + "="
	entry := prefix + value
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = entry
			return lines
		}
	}
	return append(lines, entry)
}

// splitList reads a semicolon-separated ini value.
func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ";") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// normalizeIDs validates every id, drops duplicates and keeps the order.
func normalizeIDs(ids []string, parse func(string) (string, error)) ([]string, error) {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id, err := parse(raw)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}
