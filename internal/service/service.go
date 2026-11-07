// Package service renders the systemd user units that run one instance.
package service

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/syberfoxx/gamehost/internal/atomicfile"
)

// Options describes the units of one instance.
type Options struct {
	// Binary is the absolute path of the gamehost binary systemd runs.
	Binary string
	// Name is the instance name.
	Name string
	// Home is exported as GAMEHOST_HOME so the unit reads the same data as the terminal.
	Home string
	// Display is exported as DISPLAY; Core Keeper opens a window even with -batchmode.
	Display string
	// Every is the backup window; zero writes no backup units.
	Every time.Duration
	// Keep is how many archives the backup window keeps.
	Keep int
	// Systemctl is the absolute path the backup unit uses to stop and start the server.
	Systemctl string
}

// unit is one unit file to write.
type unit struct {
	name    string
	content string
}

// UnitName returns the main service unit of one instance.
func UnitName(name string) string {
	return "gamehost-" + name + ".service"
}

// BackupUnitName returns the one-shot backup unit of one instance.
func BackupUnitName(name string) string {
	return "gamehost-" + name + "-backup.service"
}

// TimerUnitName returns the timer that triggers the backup unit.
func TimerUnitName(name string) string {
	return "gamehost-" + name + "-backup.timer"
}

// DefaultDir returns the systemd user unit directory.
func DefaultDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "systemd", "user"), nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(userHome, ".config", "systemd", "user"), nil
}

// Install writes the units of one instance and returns the files it wrote.
// It never talks to systemd: enabling and starting is the caller's decision.
func Install(dir string, opts Options) ([]string, error) {
	if !filepath.IsAbs(opts.Binary) {
		return nil, fmt.Errorf("binary path %q must be absolute", opts.Binary)
	}
	if opts.Name == "" {
		return nil, errors.New("instance name is required")
	}
	if opts.Every < 0 {
		return nil, fmt.Errorf("backup interval %s cannot be negative", opts.Every)
	}
	if opts.Every > 0 {
		if opts.Keep < 1 {
			return nil, fmt.Errorf("backup keep %d must be at least 1 with a schedule", opts.Keep)
		}
		if !filepath.IsAbs(opts.Systemctl) {
			return nil, fmt.Errorf("systemctl path %q must be absolute with a backup schedule", opts.Systemctl)
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %q: %w", dir, err)
	}

	// A schedule that was turned off must not leave an older timer behind.
	if opts.Every == 0 {
		for _, name := range []string{TimerUnitName(opts.Name), BackupUnitName(opts.Name)} {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("remove stale unit %q: %w", name, err)
			}
		}
	}

	units := []unit{{UnitName(opts.Name), mainUnit(opts)}}
	if opts.Every > 0 {
		units = append(units,
			unit{BackupUnitName(opts.Name), backupUnit(opts)},
			unit{TimerUnitName(opts.Name), timerUnit(opts)},
		)
	}

	written := make([]string, 0, len(units))
	for _, u := range units {
		path := filepath.Join(dir, u.name)
		if err := atomicfile.Replace(path, []byte(u.content), 0o644); err != nil {
			return written, fmt.Errorf("write %q: %w", path, err)
		}
		written = append(written, path)
	}
	return written, nil
}

// Remove deletes the units of one instance and returns the files it removed.
// It ignores units that were never written.
func Remove(dir, name string) ([]string, error) {
	removed := make([]string, 0, 3)
	for _, unitName := range []string{TimerUnitName(name), BackupUnitName(name), UnitName(name)} {
		path := filepath.Join(dir, unitName)
		if err := os.Remove(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return removed, fmt.Errorf("remove %q: %w", path, err)
		}
		removed = append(removed, path)
	}
	return removed, nil
}

// mainUnit supervises the foreground server, so systemd restarts a crash.
func mainUnit(opts Options) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=gamehost: dedicated server %s\n", opts.Name)
	b.WriteString("After=network-online.target\nWants=network-online.target\n\n")

	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s start %s\n", arg(opts.Binary), arg(opts.Name))
	b.WriteString("Restart=on-failure\nRestartSec=5\n")
	// mixed lets gamehost signal the game itself and gives it the full grace
	// period before systemd kills the whole control group: 60s for the save
	// plus 30s for gamehost to write the final state.
	b.WriteString("KillMode=mixed\nTimeoutStopSec=90\n")
	if opts.Home != "" {
		fmt.Fprintf(&b, "Environment=%s\n", envArg("GAMEHOST_HOME="+opts.Home))
	}
	if opts.Display != "" {
		fmt.Fprintf(&b, "Environment=%s\n", envArg("DISPLAY="+opts.Display))
	}
	b.WriteString("\n[Install]\nWantedBy=default.target\n")
	return b.String()
}

// backupUnit stops the server, archives it and brings it back. ExecStopPost
// runs even when the archive fails, so a broken backup never leaves it down.
func backupUnit(opts Options) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=gamehost: backup window for %s\n\n", opts.Name)

	b.WriteString("[Service]\n")
	b.WriteString("Type=oneshot\n")
	fmt.Fprintf(&b, "ExecStartPre=-%s --user stop %s\n", arg(opts.Systemctl), arg(UnitName(opts.Name)))
	fmt.Fprintf(&b, "ExecStart=%s backup %s -keep %d\n", arg(opts.Binary), arg(opts.Name), opts.Keep)
	fmt.Fprintf(&b, "ExecStopPost=-%s --user start %s\n", arg(opts.Systemctl), arg(UnitName(opts.Name)))
	if opts.Home != "" {
		fmt.Fprintf(&b, "Environment=%s\n", envArg("GAMEHOST_HOME="+opts.Home))
	}
	return b.String()
}

// timerUnit fires the backup unit after boot and then once per interval.
func timerUnit(opts Options) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=gamehost: periodic backup for %s\n\n", opts.Name)

	b.WriteString("[Timer]\n")
	b.WriteString("OnBootSec=5min\n")
	fmt.Fprintf(&b, "OnUnitActiveSec=%s\n", duration(opts.Every))
	fmt.Fprintf(&b, "Unit=%s\n", BackupUnitName(opts.Name))
	b.WriteString("\n[Install]\nWantedBy=timers.target\n")
	return b.String()
}

// duration renders a Go duration in the short form systemd documents.
func duration(value time.Duration) string {
	switch {
	case value%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", value/(24*time.Hour))
	case value%time.Hour == 0:
		return fmt.Sprintf("%dh", value/time.Hour)
	case value%time.Minute == 0:
		return fmt.Sprintf("%dm", value/time.Minute)
	default:
		return fmt.Sprintf("%ds", value/time.Second)
	}
}

// arg renders one ExecStart argument. systemd expands % specifiers and
// variables even inside quotes, so both are escaped; anything outside the safe
// set is wrapped in double quotes.
func arg(value string) string {
	return quote(strings.NewReplacer("%", "%%", "$", "$$").Replace(value))
}

// envArg renders one Environment assignment, where variables are not expanded.
func envArg(value string) string {
	return quote(strings.NewReplacer("%", "%%").Replace(value))
}

// quote wraps a word in double quotes when it contains anything unusual.
func quote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool { return !safeRune(r) }) < 0 {
		return value
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return `"` + escaped + `"`
}

// safeRune reports whether a character survives systemd's word splitting.
func safeRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune("_@+-=:,./", r)
}
