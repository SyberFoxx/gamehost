// Package atomicfile replaces a file without ever leaving a partial copy.
package atomicfile

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Replace writes data to path through a temporary file in the same directory.
// Rename is atomic inside a filesystem, so a reader sees either the old file or
// the new one, never a half-written one.
func Replace(path string, data []byte, perm fs.FileMode) error {
	return ReplaceFile(path, bytes.NewReader(data), perm)
}

// ReplaceFile replaces path with the contents of src, never leaving a partial copy.
// It streams, so an import does not have to hold a whole world in memory.
func ReplaceFile(path string, src io.Reader, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}
