package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/syberfoxx/gamehost/internal/atomicfile"
	"github.com/syberfoxx/gamehost/internal/backup"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runBackup archives the data directory of one instance.
func runBackup(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("backup", stderr)
	keep := fs.Int("keep", defaultBackupKeep, "backups to keep")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("backup needs exactly one instance name: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	_, err = backupInstance(s, inst, *keep, stdout)
	return err
}

// backupInstance archives the data of a stopped instance and applies retention.
func backupInstance(s *store.Store, inst instance.Instance, keep int, stdout io.Writer) (backup.Result, error) {
	release, err := requireStopped(s, inst, "backup")
	if err != nil {
		return backup.Result{}, err
	}
	defer release()

	layout := s.Layout(inst.Name)
	if err := os.MkdirAll(layout.Backups, 0o755); err != nil {
		return backup.Result{}, fmt.Errorf("create backup directory: %w", err)
	}

	path := filepath.Join(layout.Backups, time.Now().Format("2006-01-02_15-04-05")+".tar.gz")
	res, err := backup.Create(path, layout.Data)
	if err != nil {
		return backup.Result{}, fmt.Errorf("backup instance %q: %w", inst.Name, err)
	}

	removed, err := backup.Prune(layout.Backups, keep)
	if err != nil {
		return res, err
	}
	fmt.Fprintf(stdout, "backed up %s: %s (%d files, %s)\n", inst.Name, res.Path, res.Files, humanBytes(res.Bytes))
	for _, name := range removed {
		fmt.Fprintf(stdout, "removed old backup %s\n", name)
	}
	return res, nil
}

// runRestore replaces the data directory of one instance from a backup.
func runRestore(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("restore", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("restore needs an instance name and an archive: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	return restoreInstance(s, inst, fs.Arg(1), stdout)
}

// restoreInstance replaces the data of a stopped instance from an archive.
// The archive is a path or a file name inside the backup directory; the stopped
// check comes first, because it is the actionable error.
func restoreInstance(s *store.Store, inst instance.Instance, archive string, stdout io.Writer) error {
	release, err := requireStopped(s, inst, "restore")
	if err != nil {
		return err
	}
	defer release()

	layout := s.Layout(inst.Name)
	archivePath, err := resolveArchive(layout.Backups, archive)
	if err != nil {
		return err
	}

	// Keep the current world before replacing it, so a bad archive is recoverable.
	safety, err := archiveData(s, inst, "pre-restore_")
	if err != nil {
		return err
	}

	if err := backup.Restore(archivePath, layout.Data); err != nil {
		return fmt.Errorf("restore instance %q: %w", inst.Name, err)
	}

	fmt.Fprintf(stdout, "restored %s from %s\n", inst.Name, archivePath)
	if safety.Path != "" {
		fmt.Fprintf(stdout, "previous data saved as %s\n", safety.Path)
	}
	return nil
}

// runImportWorld copies an existing world save into the instance's slot.
func runImportWorld(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("import-world", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("import-world needs an instance name and a world file: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	return importWorld(s, inst, fs.Arg(1), stdout)
}

// importWorld copies one world save into the instance's slot and archives the
// current data first. The lobby reuses it after its confirmation.
func importWorld(s *store.Store, inst instance.Instance, path string, stdout io.Writer) error {
	release, err := requireStopped(s, inst, "import-world")
	if err != nil {
		return err
	}
	defer release()

	source, err := openWorld(path)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()

	safety, err := archiveData(s, inst, "pre-import_")
	if err != nil {
		return err
	}
	if safety.Path != "" {
		fmt.Fprintf(stdout, "saved the current data first: %s (%d files, %s)\n",
			safety.Path, safety.Files, humanBytes(safety.Bytes))
	}

	layout := s.Layout(inst.Name)
	worlds := filepath.Join(layout.Data, "worlds")
	if err := os.MkdirAll(worlds, 0o755); err != nil {
		return fmt.Errorf("create world directory: %w", err)
	}
	target := filepath.Join(worlds, fmt.Sprintf("%d.world.gzip", inst.World.Index))
	if err := atomicfile.ReplaceFile(target, source, 0o644); err != nil {
		return fmt.Errorf("import world into %q: %w", inst.Name, err)
	}

	fmt.Fprintf(stdout, "imported %s into %s (slot %d)\n  world: %s\nnext: gamehost start %s --detach\n",
		path, inst.Name, inst.World.Index, target, inst.Name)
	return nil
}

// openWorld opens a Core Keeper world save and checks its gzip header.
func openWorld(path string) (*os.File, error) {
	if !strings.HasSuffix(path, ".world.gzip") {
		return nil, fmt.Errorf("world %q must end in .world.gzip: %w", path, ErrUsage)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open world %q: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect world %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("world %q is not a regular file: %w", path, ErrUsage)
	}
	// The extension is the only reliable marker: current Core Keeper saves do
	// not start with the gzip magic their name suggests, so the server stays the
	// authority on the contents and an empty file is the one thing we reject.
	if info.Size() == 0 {
		_ = file.Close()
		return nil, fmt.Errorf("world %q is empty: %w", path, ErrUsage)
	}
	return file, nil
}

// resolveArchive accepts a path or a file name inside the backup directory.
func resolveArchive(dir, arg string) (string, error) {
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		return arg, nil
	}
	candidate := filepath.Join(dir, arg)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}
	return "", fmt.Errorf("archive %q not found", arg)
}
