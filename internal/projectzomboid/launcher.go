package projectzomboid

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/syberfoxx/gamehost/internal/atomicfile"
	"github.com/syberfoxx/gamehost/internal/instance"
)

// ConfigFile is the launcher json that carries the JVM arguments.
const ConfigFile = "ProjectZomboid64.json"

// heapArg is the JVM flag that sets the maximum heap.
const heapArg = "-Xmx"

// autosaveKey is the server ini key that sets the world save interval.
const autosaveKey = "SaveWorldEveryMinutes"

// Prepare writes the configured heap into the launcher json, which is the only
// place the dedicated server reads its JVM arguments from. It runs before every
// launch because `update` restores the file SteamCMD ships.
func Prepare(inst instance.Instance) error {
	// An unreadable machine size only costs the early warning: the JVM checks
	// the real limit when it starts.
	total, err := totalMemory()
	if err != nil {
		total = 0
	}
	return prepare(inst, total)
}

// Validate checks the heap against the machine without touching a file.
func (Runner) Validate(inst instance.Instance) error {
	// An unreadable machine size only costs the early warning: the JVM checks
	// the real limit when it starts.
	total, err := totalMemory()
	if err != nil {
		total = 0
	}
	_, err = checkMemory(inst, total)
	return err
}

// prepare is Prepare with the machine size given, so tests can fix it.
func prepare(inst instance.Instance, totalBytes int64) error {
	size, err := checkMemory(inst, totalBytes)
	if err != nil {
		return err
	}
	if err := writeHeap(filepath.Join(inst.InstallDir, ConfigFile), size); err != nil {
		return err
	}
	return writeAutosave(inst)
}

// checkMemory returns the normalized heap, or the reason the JVM cannot use it.
func checkMemory(inst instance.Instance, totalBytes int64) (string, error) {
	memory := inst.Memory
	if memory == "" {
		memory = instance.DefaultProjectZomboidMemory
	}
	size, bytes, err := instance.ParseMemory(memory)
	if err != nil {
		return "", err
	}
	if totalBytes > 0 && bytes > totalBytes {
		return "", fmt.Errorf("memory %s is more than this machine's %s: %w", size, humanMemory(totalBytes), instance.ErrInvalid)
	}
	return size, nil
}

// writeHeap replaces the heap of the launcher and leaves every other key alone.
// The value is written only when it changes, so a restart without a change does
// not touch the file.
func writeHeap(path, memory string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	// A map of raw values keeps keys this build does not know about, including
	// the ones a game update adds.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	var args []string
	if value, ok := doc["vmArgs"]; ok {
		if err := json.Unmarshal(value, &args); err != nil {
			return fmt.Errorf("parse vmArgs of %s: %w", path, err)
		}
	}
	want := heapArg + memory
	replaced := false
	for i, arg := range args {
		if strings.HasPrefix(arg, heapArg) {
			args[i] = want
			replaced = true
		}
	}
	if !replaced {
		args = append(args, want)
	}

	encoded, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("encode vmArgs of %s: %w", path, err)
	}
	doc["vmArgs"] = encoded
	out, err := json.MarshalIndent(doc, "", "\t")
	if err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	out = append(out, '\n')
	if bytes.Equal(out, raw) {
		return nil
	}

	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := atomicfile.Replace(path, out, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// writeAutosave applies the world autosave interval to the server ini. The
// file only exists after the first start, so a new instance skips the write and
// the value applies from the second launch. Only that one line changes.
func writeAutosave(inst instance.Instance) error {
	minutes := inst.AutosaveMinutes
	if minutes <= 0 {
		return nil
	}
	path := IniPath(inst)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %q: %w", path, err)
	}
	if current := iniValue(strings.Split(strings.TrimSuffix(string(data), "\n"), "\n"), autosaveKey); current == strconv.Itoa(minutes) {
		return nil
	}

	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	lines = setIniValue(lines, autosaveKey, strconv.Itoa(minutes))
	out := []byte(strings.Join(lines, "\n"))
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	if err := atomicfile.Replace(path, out, 0o644); err != nil {
		return fmt.Errorf("save %q: %w", path, err)
	}
	return nil
}

// totalMemory reports the physical memory of the machine in bytes.
func totalMemory() (int64, error) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse MemTotal %q: %w", fields[1], err)
		}
		return kb << 10, nil
	}
	return 0, fmt.Errorf("/proc/meminfo has no MemTotal")
}

// humanMemory rounds a size down to the largest whole unit, for a message.
func humanMemory(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%dg", bytes>>30)
	case bytes >= 1<<20:
		return fmt.Sprintf("%dm", bytes>>20)
	default:
		return fmt.Sprintf("%dk", bytes>>10)
	}
}
