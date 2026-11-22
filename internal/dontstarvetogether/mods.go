package dontstarvetogether

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/syberfoxx/gamehost/internal/atomicfile"
	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
)

// ErrInvalidMod reports a workshop id or mod name the server would not load.
var ErrInvalidMod = errors.New("invalid mod")

const maxModNameLen = 128

var (
	setupLinePattern   = regexp.MustCompile(`^\s*ServerModSetup\(\s*"([^"]+)"\s*\)\s*$`)
	overrideEntryStart = regexp.MustCompile(`^\s*\["([^"]+)"\]\s*=\s*\{`)
)

// ModSetupPath returns the install-wide file that installs Workshop mods.
func ModSetupPath(inst instance.Instance) string {
	return filepath.Join(inst.InstallDir, "mods", "dedicated_server_mods_setup.lua")
}

// ModOverridePath returns the per-shard file that enables mods.
func ModOverridePath(inst instance.Instance, shard string) string {
	return filepath.Join(ClusterDir(inst), shard, "modoverrides.lua")
}

// Mods reads the installed Workshop ids and the names enabled on each shard.
func (Runner) Mods(inst instance.Instance) (game.Mods, error) {
	setup, err := readOptional(ModSetupPath(inst))
	if err != nil {
		return game.Mods{}, err
	}
	var workshop []string
	seenWorkshop := make(map[string]bool)
	for _, line := range strings.Split(setup, "\n") {
		match := setupLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		id, err := parseWorkshopID(match[1])
		if err != nil {
			return game.Mods{}, err
		}
		if !seenWorkshop[id] {
			seenWorkshop[id] = true
			workshop = append(workshop, id)
		}
	}

	shards := []string{Shard}
	if inst.Caves {
		shards = append(shards, CavesShard)
	}
	var enabled []string
	seenEnabled := make(map[string]bool)
	for _, shard := range shards {
		data, err := readOptional(ModOverridePath(inst, shard))
		if err != nil {
			return game.Mods{}, err
		}
		for _, line := range strings.Split(data, "\n") {
			match := overrideEntryStart.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			name, err := parseModName(match[1])
			if err != nil {
				return game.Mods{}, err
			}
			if !seenEnabled[name] {
				seenEnabled[name] = true
				enabled = append(enabled, name)
			}
		}
	}
	return game.Mods{Workshop: workshop, Enabled: enabled}, nil
}

// SaveMods installs the Workshop ids and enables the listed names on every
// shard. A numeric mod name is the Workshop mod workshop-<id>, so either
// `mods add <id>` or `mods add --workshop <id>` gives a working installation.
func (Runner) SaveMods(inst instance.Instance, mods game.Mods) error {
	workshop, err := normalizeMods(mods.Workshop, parseWorkshopID)
	if err != nil {
		return err
	}
	enabled, err := normalizeMods(mods.Enabled, parseModName)
	if err != nil {
		return err
	}
	// Installing and enabling are separate files but one operation for the
	// operator: a Workshop id is enabled as workshop-<id>, and an enabled
	// Workshop name is installed. Names the player disabled stay out of the
	// enable list because Mods() reports every name it finds.
	for _, id := range workshop {
		name := "workshop-" + id
		if !contains(enabled, name) {
			enabled = append(enabled, name)
		}
	}
	for _, name := range enabled {
		if id, ok := strings.CutPrefix(name, "workshop-"); ok {
			if id, err := parseWorkshopID(id); err == nil && !contains(workshop, id) {
				workshop = append(workshop, id)
			}
		}
	}

	setupPath := ModSetupPath(inst)
	setup, err := readOptional(setupPath)
	if err != nil {
		return err
	}
	setupLines := make([]string, 0, len(strings.Split(setup, "\n"))+len(workshop))
	for _, line := range strings.Split(strings.TrimSuffix(setup, "\n"), "\n") {
		if line != "" && setupLinePattern.MatchString(line) {
			continue
		}
		setupLines = append(setupLines, line)
	}
	for _, id := range workshop {
		setupLines = append(setupLines, fmt.Sprintf("ServerModSetup(%q)", id))
	}
	if err := writeOptional(setupPath, strings.Join(setupLines, "\n")); err != nil {
		return err
	}

	shards := []string{Shard}
	if inst.Caves {
		shards = append(shards, CavesShard)
	}
	for _, shard := range shards {
		path := ModOverridePath(inst, shard)
		data, err := readOptional(path)
		if err != nil {
			return err
		}
		if err := writeOptional(path, updateOverrides(data, enabled)); err != nil {
			return err
		}
	}
	return nil
}

// parseWorkshopID validates a numeric Steam Workshop id.
func parseWorkshopID(value string) (string, error) {
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

// parseModName validates a mod name. A bare Workshop id names workshop-<id>.
func parseModName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len(name) > maxModNameLen {
		return "", fmt.Errorf("mod name %q must be 1-%d characters: %w", value, maxModNameLen, ErrInvalidMod)
	}
	if digits(name) {
		return "workshop-" + name, nil
	}
	for _, r := range name {
		isLetter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		isDigit := r >= '0' && r <= '9'
		if !isLetter && !isDigit && r != '_' && r != '-' && r != '.' {
			return "", fmt.Errorf("mod name %q may only use letters, digits, _, - or .: %w", value, ErrInvalidMod)
		}
	}
	return name, nil
}

// normalizeMods validates every id, drops duplicates and keeps the order.
func normalizeMods(values []string, parse func(string) (string, error)) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, raw := range values {
		value, err := parse(raw)
		if err != nil {
			return nil, err
		}
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out, nil
}

// digits reports whether value is a non-empty decimal number.
func digits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

// contains reports whether list holds value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// updateOverrides keeps every existing entry in wanted and drops the rest.
// Entries the player configured keep their configuration_options untouched.
func updateOverrides(data string, wanted []string) string {
	lines := strings.Split(strings.TrimSuffix(data, "\n"), "\n")
	if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
		lines = []string{"return {", "}"}
	}
	out := make([]string, 0, len(lines)+len(wanted))
	seen := make(map[string]bool, len(wanted))
	for i := 0; i < len(lines); i++ {
		match := overrideEntryStart.FindStringSubmatch(lines[i])
		if match == nil {
			out = append(out, lines[i])
			continue
		}
		name, err := parseModName(match[1])
		if err != nil {
			name = match[1]
		}
		end := entryEnd(lines, i)
		if contains(wanted, name) {
			out = append(out, lines[i:end]...)
			seen[name] = true
		}
		i = end - 1
	}
	missing := make([]string, 0, len(wanted))
	for _, name := range wanted {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return strings.Join(out, "\n") + "\n"
	}
	insert := -1
	for i := len(out) - 1; i >= 0; i-- {
		if strings.TrimSpace(out[i]) == "}" {
			insert = i
			break
		}
	}
	entries := make([]string, 0, len(missing))
	for _, name := range missing {
		entries = append(entries, fmt.Sprintf("  [%q] = { enabled = true },", name))
	}
	if insert < 0 {
		out = append(out, "return {")
		out = append(out, entries...)
		out = append(out, "}")
		return strings.Join(out, "\n") + "\n"
	}
	result := append([]string{}, out[:insert]...)
	result = append(result, entries...)
	result = append(result, out[insert:]...)
	return strings.Join(result, "\n") + "\n"
}

// entryEnd returns the index just past one mod table, including nested tables.
func entryEnd(lines []string, start int) int {
	depth := 0
	for i := start; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{") - strings.Count(lines[i], "}")
		if depth <= 0 {
			return i + 1
		}
	}
	return len(lines)
}

// readOptional returns an empty string when the file does not exist yet.
func readOptional(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), nil
	}
	if os.IsNotExist(err) {
		return "", nil
	}
	return "", fmt.Errorf("read %q: %w", path, err)
}

// writeOptional creates the parent directory and replaces the file atomically.
func writeOptional(path, data string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create %q: %w", filepath.Dir(path), err)
	}
	if err := atomicfile.Replace(path, []byte(strings.Join(nonEmptyLines(data), "\n")+"\n"), 0o644); err != nil {
		return fmt.Errorf("save %q: %w", path, err)
	}
	return nil
}

// nonEmptyLines drops the single empty line of an empty file.
func nonEmptyLines(data string) []string {
	if data == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(data, "\n"), "\n")
}
