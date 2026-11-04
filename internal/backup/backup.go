// Package backup archives and restores the data directory of an instance.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// archiveSuffix marks every file this package creates.
const archiveSuffix = ".tar.gz"

// Result describes a written archive.
type Result struct {
	Path  string
	Files int
	Bytes int64
}

// Create writes a gzip-compressed tar of dir into archivePath.
// The archive replaces archivePath atomically: it is written to a temporary
// file in the same directory and renamed.
func Create(archivePath, dir string) (Result, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return Result{}, fmt.Errorf("inspect %q: %w", dir, err)
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("%q is not a directory", dir)
	}

	tmp, err := os.CreateTemp(filepath.Dir(archivePath), ".backup-*.tmp")
	if err != nil {
		return Result{}, fmt.Errorf("create archive: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName) // no-op after a successful rename
	}()

	gz := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gz)

	res := Result{Path: archivePath}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			header, err := tar.FileInfoHeader(fileInfo, "")
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(rel) + "/"
			return tw.WriteHeader(header)
		case fileInfo.Mode().IsRegular():
			return writeFile(tw, path, rel, fileInfo, &res)
		default:
			// Symlinks, sockets and devices are not part of a restorable world.
			return nil
		}
	})
	if err != nil {
		return Result{}, fmt.Errorf("archive %q: %w", dir, err)
	}

	if err := tw.Close(); err != nil {
		return Result{}, fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return Result{}, fmt.Errorf("close gzip: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return Result{}, fmt.Errorf("sync archive: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return Result{}, fmt.Errorf("close archive: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return Result{}, fmt.Errorf("chmod archive: %w", err)
	}
	if err := os.Rename(tmpName, archivePath); err != nil {
		return Result{}, fmt.Errorf("replace %q: %w", archivePath, err)
	}
	return res, nil
}

// writeFile appends one regular file to the tar stream.
func writeFile(tw *tar.Writer, path, rel string, info fs.FileInfo, res *Result) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(rel)
	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("copy %q: %w", path, err)
	}
	res.Files++
	res.Bytes += info.Size()
	return nil
}

// Restore extracts archivePath over dir, replacing its current contents.
// The previous directory is kept next to it until the swap succeeds.
func Restore(archivePath, dir string) error {
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent of %q: %w", dir, err)
	}

	staging, err := os.MkdirTemp(parent, ".restore-*")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }() // no-op after a successful rename

	if err := extract(archivePath, staging); err != nil {
		return err
	}

	previous := dir + ".pre-restore-" + time.Now().Format("20060102-150405")
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, previous); err != nil {
			return fmt.Errorf("move current data aside: %w", err)
		}
	}
	if err := os.Rename(staging, dir); err != nil {
		// Put the old data back so a failed restore never loses the world.
		_ = os.Rename(previous, dir)
		return fmt.Errorf("install restored data: %w", err)
	}
	_ = os.RemoveAll(previous)
	return nil
}

// extract unpacks a tar.gz into dir, rejecting entries that escape it.
func extract(archivePath, dir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive %q: %w", archivePath, err)
	}
	defer func() { _ = file.Close() }()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("read gzip %q: %w", archivePath, err)
	}
	defer func() { _ = gz.Close() }()

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar %q: %w", archivePath, err)
		}

		name := filepath.Clean(filepath.FromSlash(header.Name))
		if name == "." {
			continue
		}
		if !filepath.IsLocal(name) {
			return fmt.Errorf("archive entry %q escapes the restore directory", header.Name)
		}
		target := filepath.Join(dir, name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("create %q: %w", target, err)
			}
		case tar.TypeReg:
			if err := writeExtracted(reader, target, header.Mode); err != nil {
				return err
			}
		default:
			return fmt.Errorf("archive entry %q has unsupported type %q", header.Name, header.Typeflag)
		}
	}
}

// writeExtracted creates one regular file from the archive.
func writeExtracted(reader io.Reader, target string, mode int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create parent of %q: %w", target, err)
	}
	// The file mode comes from the archive; keep it inside sane permissions.
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fs.FileMode(mode)&0o777)
	if err != nil {
		return fmt.Errorf("create %q: %w", target, err)
	}
	if _, err := io.Copy(file, reader); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %q: %w", target, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %q: %w", target, err)
	}
	return nil
}

// Archive describes one file in a backup directory.
type Archive struct {
	Name     string    `json:"name"`
	Path     string    `json:"-"`
	Bytes    int64     `json:"bytes"`
	Modified time.Time `json:"modified"`
}

// List returns the archives in dir, newest first. A missing directory has no
// archives, which is not an error.
func List(dir string) ([]Archive, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list backups %q: %w", dir, err)
	}

	archives := make([]Archive, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), archiveSuffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect backup %q: %w", entry.Name(), err)
		}
		archives = append(archives, Archive{
			Name:     entry.Name(),
			Path:     filepath.Join(dir, entry.Name()),
			Bytes:    info.Size(),
			Modified: info.ModTime(),
		})
	}

	slices.SortFunc(archives, func(a, b Archive) int {
		if c := b.Modified.Compare(a.Modified); c != 0 {
			return c
		}
		return strings.Compare(b.Name, a.Name)
	})
	return archives, nil
}

// Newest returns the modification time of the newest archive in dir.
func Newest(dir string) (time.Time, bool, error) {
	archives, err := List(dir)
	if err != nil || len(archives) == 0 {
		return time.Time{}, false, err
	}
	return archives[0].Modified, true, nil
}

// Prune deletes the oldest archives, keeping the newest keep files.
// It returns the removed file names.
func Prune(dir string, keep int) ([]string, error) {
	if keep < 1 {
		return nil, fmt.Errorf("keep must be at least 1")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list backups %q: %w", dir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), archiveSuffix) {
			continue
		}
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	if len(names) <= keep {
		return nil, nil
	}

	removed := names[:len(names)-keep]
	for _, name := range removed {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, fmt.Errorf("remove backup %q: %w", name, err)
		}
	}
	return removed, nil
}
