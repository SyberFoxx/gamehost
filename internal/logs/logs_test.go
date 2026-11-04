package logs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// writeLog creates a log file with the given content.
func writeLog(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// syncBuffer is a bytes.Buffer safe for a writer and a reader in different goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// covers picking the newest log and the empty cases
func TestLatest(t *testing.T) {
	t.Parallel()

	if _, err := Latest(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrNoLogs) {
		t.Errorf("Latest() error = %v, want ErrNoLogs for a missing directory", err)
	}

	dir := t.TempDir()
	if _, err := Latest(dir); !errors.Is(err, ErrNoLogs) {
		t.Errorf("Latest() error = %v, want ErrNoLogs for an empty directory", err)
	}

	writeLog(t, dir, "2026-11-04_10-00-00.log", "old")
	writeLog(t, dir, "2026-11-04_14-01-14.log", "new")
	writeLog(t, dir, "notes.txt", "ignore me")

	got, err := Latest(dir)
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if want := filepath.Join(dir, "2026-11-04_14-01-14.log"); got != want {
		t.Errorf("Latest() = %q, want %q", got, want)
	}
}

// covers log retention
func TestPrune(t *testing.T) {
	t.Parallel()

	if removed, err := Prune(filepath.Join(t.TempDir(), "missing"), 2); err != nil || removed != nil {
		t.Errorf("Prune(missing) = %v, %v; want nil, nil", removed, err)
	}

	dir := t.TempDir()
	writeLog(t, dir, "2026-11-01_10-00-00.log", "a")
	writeLog(t, dir, "2026-11-02_10-00-00.log", "b")
	writeLog(t, dir, "2026-11-03_10-00-00.log", "c")
	writeLog(t, dir, "notes.txt", "not a log")

	removed, err := Prune(dir, 2)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if want := []string{"2026-11-01_10-00-00.log"}; !slices.Equal(removed, want) {
		t.Errorf("Prune() removed %v, want %v", removed, want)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("directory holds %d entries, want the newest two logs and notes.txt", len(entries))
	}

	// keep below one still protects the current log.
	if removed, err := Prune(dir, 0); err != nil || len(removed) != 1 {
		t.Errorf("Prune(0) = %v, %v; want one removal", removed, err)
	}
}

// covers the ring buffer limits
func TestTail(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeLog(t, dir, "run.log", "one\ntwo\nthree\nfour\nfive\n")

	tests := []struct {
		lines int
		want  string
	}{
		{2, "four\nfive\n"},
		{3, "three\nfour\nfive\n"},
		{99, "one\ntwo\nthree\nfour\nfive\n"},
		{0, "one\ntwo\nthree\nfour\nfive\n"},
	}

	for _, tt := range tests {
		var out bytes.Buffer
		if err := Tail(&out, path, tt.lines); err != nil {
			t.Fatalf("Tail(%d) error = %v", tt.lines, err)
		}
		if got := out.String(); got != tt.want {
			t.Errorf("Tail(%d) = %q, want %q", tt.lines, got, tt.want)
		}
	}
}

// covers lines longer than the scanner default
func TestTailLongLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	long := strings.Repeat("x", 100*1024)
	path := writeLog(t, dir, "run.log", long+"\nlast\n")

	var out bytes.Buffer
	if err := Tail(&out, path, 1); err != nil {
		t.Fatalf("Tail() error = %v", err)
	}
	if got := out.String(); got != "last\n" {
		t.Errorf("Tail() = %q, want the last line", got)
	}
}

// covers the initial tail and bytes appended while following
func TestFollow(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeLog(t, dir, "run.log", "first\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- Follow(ctx, out, path, 10) }()

	waitFor(t, out, "first\n")

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("second\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	waitFor(t, out, "second\n")

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Follow() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Follow() did not return after cancellation")
	}
}

// waitFor blocks until the buffer contains want, then fails after a deadline.
func waitFor(t *testing.T, out *syncBuffer, want string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("output %q never contained %q", out.String(), want)
}
