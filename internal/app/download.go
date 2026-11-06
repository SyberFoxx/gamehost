package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/syberfoxx/gamehost/internal/backup"
	"github.com/syberfoxx/gamehost/internal/games"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/steamcmd"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runDownload installs or updates the server files of one instance.
func runDownload(ctx context.Context, command string, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(command, stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%s needs exactly one instance name: %w", command, ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	inst, err := s.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	return installInstance(ctx, command, s, inst, stdout)
}

// installInstance installs or updates the server files of one instance.
// The lobby reuses it with a writer that turns the SteamCMD output into events.
func installInstance(ctx context.Context, command string, s *store.Store, inst instance.Instance, stdout io.Writer) error {
	// An update rewrites the server files under a running process, and the
	// archive is only consistent while the world is closed.
	if command == "update" {
		release, err := requireStopped(s, inst, command)
		if err != nil {
			return err
		}
		defer release()
		res, err := archiveData(s, inst, "pre-update_")
		if err != nil {
			return err
		}
		if res.Path != "" {
			fmt.Fprintf(stdout, "backed up %s before the update: %s (%d files, %s)\n",
				inst.Name, res.Path, res.Files, humanBytes(res.Bytes))
		}
	}

	runner, err := newRunner()
	if err != nil {
		return err
	}

	driver, err := games.Lookup(inst.Game)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s: downloading server files into %s\n", command, inst.InstallDir)
	if err := runner.Run(ctx, stdout, driver.Installer.InstallArgs(inst.InstallDir)...); err != nil {
		return fmt.Errorf("%s instance %q: %w", command, inst.Name, err)
	}
	serverPath := driver.Installer.ServerPath(inst.InstallDir)
	if _, err := os.Stat(serverPath); err != nil {
		return fmt.Errorf("%s instance %q: server binary missing after download: %w", command, inst.Name, err)
	}

	fmt.Fprintf(stdout, "%s: %s is ready\n", command, serverPath)
	return nil
}

// archiveData writes a safety archive of the data directory.
// It returns an empty result when the instance has no data yet.
func archiveData(s *store.Store, inst instance.Instance, prefix string) (backup.Result, error) {
	layout := s.Layout(inst.Name)
	entries, err := os.ReadDir(layout.Data)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return backup.Result{}, nil
		}
		return backup.Result{}, fmt.Errorf("inspect data directory of %q: %w", inst.Name, err)
	}
	if len(entries) == 0 {
		return backup.Result{}, nil
	}
	if err := os.MkdirAll(layout.Backups, 0o755); err != nil {
		return backup.Result{}, fmt.Errorf("create backup directory: %w", err)
	}

	path := filepath.Join(layout.Backups, prefix+time.Now().Format("2006-01-02_15-04-05")+".tar.gz")
	res, err := backup.Create(path, layout.Data)
	if err != nil {
		return backup.Result{}, fmt.Errorf("backup %q: %w", inst.Name, err)
	}
	return res, nil
}

// newRunner locates SteamCMD and wraps it in a runner.
func newRunner() (steamcmd.Runner, error) {
	path, err := steamcmd.Locate()
	if err != nil {
		return steamcmd.Runner{}, fmt.Errorf("%w: install SteamCMD or set %s", err, steamcmd.EnvPath)
	}
	return steamcmd.Runner{Path: path}, nil
}
