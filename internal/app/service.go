package app

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/syberfoxx/gamehost/internal/home"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/process"
	"github.com/syberfoxx/gamehost/internal/service"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runService installs or removes the systemd units of one instance.
func runService(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("service needs a subcommand: install or uninstall: %w", ErrUsage)
	}
	switch args[0] {
	case "install":
		return runServiceInstall(args[1:], stdout, stderr)
	case "uninstall":
		return runServiceUninstall(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown service subcommand %q (want install or uninstall): %w", args[0], ErrUsage)
	}
}

// serviceInstallOptions are the flags of `service install`.
type serviceInstallOptions struct {
	Enable    bool
	Dir       string
	Binary    string
	Systemctl string
}

// runServiceInstall writes the units of one instance and can enable them.
func runServiceInstall(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("service install", stderr)
	enable := fs.Bool("enable", false, "enable and start the units now")
	dir := fs.String("dir", "", "unit directory (default: the systemd user directory)")
	binary := fs.String("binary", "", "gamehost binary the unit runs (default: this executable)")
	systemctl := fs.String("systemctl", "", "systemctl used by the backup unit (default: from PATH)")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("service install needs exactly one instance name: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	return installService(s, inst, serviceInstallOptions{
		Enable:    *enable,
		Dir:       *dir,
		Binary:    *binary,
		Systemctl: *systemctl,
	}, stdout, stderr)
}

// installService writes the units of one instance and optionally enables them.
// The lobby reuses it with the same defaults the command uses.
func installService(s *store.Store, inst instance.Instance, opts serviceInstallOptions, stdout, stderr io.Writer) error {
	unitDir, err := resolveDir(opts.Dir)
	if err != nil {
		return err
	}
	exe, err := resolveBinary(opts.Binary)
	if err != nil {
		return err
	}
	ctl, err := resolveSystemctl(opts.Systemctl)
	if err != nil {
		return err
	}
	root, err := home.Dir()
	if err != nil {
		return err
	}

	every := inst.Backup.Interval()
	// Turning the schedule off removes the timer file, so disable it first or
	// systemd keeps a dangling link and fires a missing unit.
	if every == 0 {
		timer := service.TimerUnitName(inst.Name)
		if _, err := os.Stat(filepath.Join(unitDir, timer)); err == nil && ctl != "" {
			if err := runSystemctl(ctl, "disable", "--now", timer); err != nil {
				fmt.Fprintf(stderr, "warning: %v\n", err)
			}
		}
	}

	unit := service.Options{
		Binary:    exe,
		Name:      inst.Name,
		Home:      root,
		Display:   os.Getenv("DISPLAY"),
		Every:     every,
		Keep:      inst.Backup.Retention(),
		Systemctl: ctl,
	}
	written, err := service.Install(unitDir, unit)
	if err != nil {
		return err
	}
	for _, path := range written {
		fmt.Fprintln(stdout, "wrote", path)
	}
	if strings.Contains(exe, string(filepath.Separator)+"go-build") {
		fmt.Fprintf(stdout, "note: %s is a temporary go run binary; build one with: make build, then pass --binary\n", exe)
	}

	if !opts.Enable {
		fmt.Fprintf(stdout, "\nnext:\n  systemctl --user daemon-reload\n  systemctl --user enable --now %s\n", service.UnitName(inst.Name))
		if every > 0 {
			fmt.Fprintf(stdout, "  systemctl --user enable --now %s\n", service.TimerUnitName(inst.Name))
		}
		return nil
	}

	if ctl == "" {
		return fmt.Errorf("service install: --enable needs systemctl in PATH: %w", ErrUnhealthy)
	}
	if err := runSystemctl(ctl, "daemon-reload"); err != nil {
		return fmt.Errorf("units written, but enabling failed: %w", err)
	}
	if err := runSystemctl(ctl, "enable", service.UnitName(inst.Name)); err != nil {
		return fmt.Errorf("units written, but enabling failed: %w", err)
	}
	if every > 0 {
		if err := runSystemctl(ctl, "enable", service.TimerUnitName(inst.Name)); err != nil {
			return fmt.Errorf("units written, but enabling the timer failed: %w", err)
		}
	}

	state, err := s.LoadState(inst.Name)
	if err != nil {
		return err
	}
	active := runUnitActive(ctl, service.UnitName(inst.Name))
	switch {
	case active:
		fmt.Fprintf(stdout, "\n%s is already running under systemd\n", service.UnitName(inst.Name))
	case state.Running() && process.Alive(state.PID):
		// Never kill a server the user started by hand: enable only, then let
		// them hand it over when they are ready.
		fmt.Fprintf(stdout, "\n%s is running outside systemd (pid %d); the unit is enabled but not started\n", inst.Name, state.PID)
		fmt.Fprintf(stdout, "hand it over with:\n  gamehost stop %s\n  systemctl --user start %s\n", inst.Name, service.UnitName(inst.Name))
	default:
		if err := runSystemctl(ctl, "start", service.UnitName(inst.Name)); err != nil {
			return fmt.Errorf("units enabled, but starting failed: %w", err)
		}
		fmt.Fprintf(stdout, "\nstarted %s\n", service.UnitName(inst.Name))
	}
	if every > 0 {
		if err := runSystemctl(ctl, "start", service.TimerUnitName(inst.Name)); err != nil {
			return fmt.Errorf("timer enabled, but starting it failed: %w", err)
		}
		fmt.Fprintf(stdout, "backup every %s, keeping %d archives\n", every, inst.Backup.Retention())
	}
	fmt.Fprintf(stdout, "logs: journalctl --user -u %s -f\n", service.UnitName(inst.Name))
	return nil
}

// runServiceUninstall disables and removes the units of one name.
// It works even when the instance configuration is already gone.
func runServiceUninstall(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("service uninstall", stderr)
	dir := fs.String("dir", "", "unit directory (default: the systemd user directory)")
	systemctl := fs.String("systemctl", "", "systemctl used to disable the units (default: from PATH)")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("service uninstall needs exactly one instance name: %w", ErrUsage)
	}
	return uninstallService(*dir, *systemctl, fs.Arg(0), stdout, stderr)
}

// uninstallService disables and removes the units of one name.
func uninstallService(dir, systemctl, name string, stdout, stderr io.Writer) error {
	unitDir, err := resolveDir(dir)
	if err != nil {
		return err
	}
	ctl, err := resolveSystemctl(systemctl)
	if err != nil {
		return err
	}

	for _, unit := range []string{service.TimerUnitName(name), service.BackupUnitName(name), service.UnitName(name)} {
		if _, err := os.Stat(filepath.Join(unitDir, unit)); err != nil {
			continue
		}
		if ctl == "" {
			fmt.Fprintf(stderr, "warning: systemctl not found; %s stays enabled in systemd\n", unit)
			continue
		}
		if err := runSystemctl(ctl, "disable", "--now", unit); err != nil {
			fmt.Fprintf(stderr, "warning: %v\n", err)
		}
	}

	removed, err := service.Remove(unitDir, name)
	if err != nil {
		return err
	}
	if len(removed) == 0 {
		fmt.Fprintf(stdout, "%s has no installed units in %s\n", name, unitDir)
		return nil
	}
	for _, path := range removed {
		fmt.Fprintln(stdout, "removed", path)
	}
	if ctl != "" {
		if err := runSystemctl(ctl, "daemon-reload"); err != nil {
			fmt.Fprintf(stderr, "warning: %v\n", err)
		}
	}
	return nil
}

// resolveDir returns the unit directory, absolute.
func resolveDir(dir string) (string, error) {
	if dir == "" {
		resolved, err := service.DefaultDir()
		if err != nil {
			return "", err
		}
		dir = resolved
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve unit directory: %w", err)
	}
	return abs, nil
}

// resolveBinary returns the binary the units execute, absolute and present.
func resolveBinary(binary string) (string, error) {
	exe := binary
	if exe == "" {
		resolved, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("resolve the gamehost binary: %w", err)
		}
		exe = resolved
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return "", fmt.Errorf("resolve the gamehost binary: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("gamehost binary %q: %w", abs, err)
	}
	return abs, nil
}

// resolveSystemctl returns the systemctl path, or empty when it is not installed.
func resolveSystemctl(systemctl string) (string, error) {
	value := systemctl
	if value == "" {
		found, err := exec.LookPath("systemctl")
		if err != nil {
			return "", nil
		}
		value = found
	} else if !filepath.IsAbs(value) {
		found, err := exec.LookPath(value)
		if err != nil {
			return "", fmt.Errorf("resolve systemctl %q: %w", value, err)
		}
		value = found
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve systemctl: %w", err)
	}
	return abs, nil
}

// runSystemctl runs one systemd user command and reports its output on failure.
func runSystemctl(ctl string, args ...string) error {
	full := append([]string{"--user"}, args...)
	out, err := exec.Command(ctl, full...).CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(out))
	if message == "" {
		return fmt.Errorf("%s %s: %w", ctl, strings.Join(full, " "), err)
	}
	return fmt.Errorf("%s %s: %w: %s", ctl, strings.Join(full, " "), err, message)
}

// runUnitActive reports whether systemd considers the unit running.
func runUnitActive(ctl, unit string) bool {
	return exec.Command(ctl, "--user", "is-active", "--quiet", unit).Run() == nil
}
