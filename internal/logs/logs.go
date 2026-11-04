// Package logs reads and follows the log file of a server instance.
package logs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// ErrNoLogs reports a directory without any log file yet.
var ErrNoLogs = errors.New("no logs yet")

// followInterval is how often Follow checks for new bytes.
const followInterval = 250 * time.Millisecond

// Latest returns the newest log file in dir.
// Names are timestamps, so lexicographic order is chronological order.
func Latest(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", ErrNoLogs
		}
		return "", fmt.Errorf("list logs %q: %w", dir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return "", ErrNoLogs
	}
	slices.Sort(names)
	return filepath.Join(dir, names[len(names)-1]), nil
}

// Prune removes the oldest log files in dir, keeping the newest keep.
// It returns the names it removed.
func Prune(dir string, keep int) ([]string, error) {
	if keep < 1 {
		keep = 1
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list logs %q: %w", dir, err)
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".log" {
			continue
		}
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	if len(names) <= keep {
		return nil, nil
	}

	removed := make([]string, 0, len(names)-keep)
	for _, name := range names[:len(names)-keep] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, fmt.Errorf("remove log %q: %w", name, err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}

// Tail writes the last lines of a file.
// It keeps at most lines in memory, so following a large log stays cheap.
func Tail(w io.Writer, path string, lines int) error {
	if lines <= 0 {
		lines = 100
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	ring := make([][]byte, lines)
	total := 0
	scanner := bufio.NewScanner(file)
	// Unity writes very long lines; the default 64 KiB token limit is too small.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		slot := total % lines
		ring[slot] = append(ring[slot][:0], scanner.Bytes()...)
		total++
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}

	start := 0
	if total > lines {
		start = total - lines
	}
	for i := start; i < total; i++ {
		if _, err := fmt.Fprintln(w, string(ring[i%lines])); err != nil {
			return fmt.Errorf("write tail: %w", err)
		}
	}
	return nil
}

// Follow prints the current tail and then every new byte until ctx ends.
func Follow(ctx context.Context, w io.Writer, path string, lines int) error {
	if err := Tail(w, path, lines); err != nil {
		return err
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat %q: %w", path, err)
	}
	offset := info.Size()

	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()
	buf := make([]byte, 32*1024)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		if err := copyFrom(w, file, &offset, buf); err != nil {
			return err
		}
	}
}

// copyFrom writes every byte available after offset and advances it.
func copyFrom(w io.Writer, file *os.File, offset *int64, buf []byte) error {
	for {
		n, err := file.ReadAt(buf, *offset)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("write follow: %w", writeErr)
			}
			*offset += int64(n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				// End of the file: wait for the next tick.
				return nil
			}
			return fmt.Errorf("read follow: %w", err)
		}
	}
}
