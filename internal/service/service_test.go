package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers the unit names systemctl receives
func TestUnitNames(t *testing.T) {
	t.Parallel()

	if got := UnitName("corekeeper"); got != "gamehost-corekeeper.service" {
		t.Errorf("UnitName() = %q", got)
	}
	if got := BackupUnitName("corekeeper"); got != "gamehost-corekeeper-backup.service" {
		t.Errorf("BackupUnitName() = %q", got)
	}
	if got := TimerUnitName("corekeeper"); got != "gamehost-corekeeper-backup.timer" {
		t.Errorf("TimerUnitName() = %q", got)
	}
}

// covers the only unit an install writes
func TestInstallWritesMainUnit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	written, err := Install(dir, Options{
		Binary:  "/usr/local/bin/gamehost",
		Name:    "corekeeper",
		Home:    "/home/ramses/.local/share/gamehost",
		Display: ":0",
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(written) != 1 || filepath.Base(written[0]) != "gamehost-corekeeper.service" {
		t.Fatalf("written = %v, want only the main unit", written)
	}

	content := readUnit(t, written[0])
	for _, want := range []string{
		"[Unit]\n",
		"Description=gamehost: dedicated server corekeeper",
		"Type=simple",
		"ExecStart=/usr/local/bin/gamehost start corekeeper",
		"Restart=on-failure",
		"RestartSec=5",
		"KillMode=mixed",
		"TimeoutStopSec=90",
		"Environment=GAMEHOST_HOME=/home/ramses/.local/share/gamehost",
		"Environment=DISPLAY=:0",
		"WantedBy=default.target",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("main unit missing %q:\n%s", want, content)
		}
	}
}

// covers a stale backup timer from an older install being removed
func TestInstallRemovesStaleBackupUnits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{TimerUnitName("corekeeper"), BackupUnitName("corekeeper")} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("[Unit]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	written, err := Install(dir, Options{
		Binary: "/usr/local/bin/gamehost",
		Name:   "corekeeper",
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("written = %v, want only the main unit", written)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != UnitName("corekeeper") {
		t.Errorf("units = %v, want only the main service", entries)
	}
}

// covers paths with spaces and percent signs in unit arguments
func TestInstallQuotesUnsafePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	written, err := Install(dir, Options{
		Binary: "/tmp/my build/gamehost%20",
		Name:   "corekeeper",
		Home:   "/home/ra mses/100% data",
	})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	main := readUnit(t, written[0])
	if !strings.Contains(main, `ExecStart="/tmp/my build/gamehost%%20" start corekeeper`) {
		t.Errorf("main unit does not escape the binary:\n%s", main)
	}
	if !strings.Contains(main, `Environment="GAMEHOST_HOME=/home/ra mses/100%% data"`) {
		t.Errorf("main unit does not escape the home:\n%s", main)
	}
}

// covers rejecting options that would write a broken unit
func TestInstallRejectsBadOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts Options
	}{
		{"relative binary", Options{Binary: "bin/gamehost", Name: "corekeeper"}},
		{"empty name", Options{Binary: "/usr/local/bin/gamehost"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			written, err := Install(t.TempDir(), tt.opts)
			if err == nil {
				t.Errorf("Install(%+v) error = nil, want a failure", tt.opts)
			}
			if written != nil {
				t.Errorf("written = %v, want nothing", written)
			}
		})
	}
}

// covers removing every unit and tolerating missing ones
func TestRemove(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if _, err := Install(dir, Options{
		Binary: "/usr/local/bin/gamehost",
		Name:   "corekeeper",
	}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	removed, err := Remove(dir, "corekeeper")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("removed = %v, want the main unit", removed)
	}
	for _, path := range removed {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s still exists", path)
		}
	}

	removed, err = Remove(dir, "corekeeper")
	if err != nil {
		t.Fatalf("second Remove() error = %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want nothing", removed)
	}
}

// covers the systemd user directory resolution
func TestDefaultDir(t *testing.T) {
	t.Run("xdg config home", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/tmp/config")

		got, err := DefaultDir()
		if err != nil {
			t.Fatalf("DefaultDir() error = %v", err)
		}
		if got != "/tmp/config/systemd/user" {
			t.Errorf("DefaultDir() = %q", got)
		}
	})

	t.Run("relative xdg is ignored", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "relative")
		t.Setenv("HOME", "/home/test")

		got, err := DefaultDir()
		if err != nil {
			t.Fatalf("DefaultDir() error = %v", err)
		}
		if got != "/home/test/.config/systemd/user" {
			t.Errorf("DefaultDir() = %q", got)
		}
	})
}

// readUnit returns the content of one written unit file.
func readUnit(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
