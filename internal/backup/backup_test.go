package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTree creates files below dir from a name-to-content map.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// covers a create and restore round trip
func TestCreateAndRestore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	data := filepath.Join(root, "data")
	writeTree(t, data, map[string]string{
		"ServerConfig.json":    `{"world": 0}`,
		"worlds/0.world.gzip":  "world",
		"mods/config/skip.txt": "nested",
	})

	archive := filepath.Join(root, "backups", "run.tar.gz")
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Create(archive, data)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if res.Files != 3 || res.Bytes == 0 {
		t.Errorf("Result = %+v, want three files with bytes", res)
	}
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("archive mode = %o, want 600", perm)
	}

	if err := os.RemoveAll(data); err != nil {
		t.Fatal(err)
	}
	if err := Restore(archive, data); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	for name, want := range map[string]string{
		"ServerConfig.json":    `{"world": 0}`,
		"worlds/0.world.gzip":  "world",
		"mods/config/skip.txt": "nested",
	} {
		got, err := os.ReadFile(filepath.Join(data, name))
		if err != nil {
			t.Fatalf("read %q: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// covers restoring over an existing directory
func TestRestoreReplacesExistingData(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	data := filepath.Join(root, "data")
	writeTree(t, data, map[string]string{"worlds/0.world.gzip": "old"})

	archive := filepath.Join(root, "backup.tar.gz")
	if _, err := Create(archive, data); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(data, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTree(t, data, map[string]string{"worlds/0.world.gzip": "new"})

	if err := Restore(archive, data); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "stale.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale file survived the restore: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(data, "worlds/0.world.gzip"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old" {
		t.Errorf("world = %q, want the archived copy", got)
	}
}

// covers rejecting an archive entry that escapes the target directory
func TestRestoreRejectsEscapingEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	archive := filepath.Join(root, "evil.tar.gz")
	writeEvilArchive(t, archive, "../escaped.txt")

	data := filepath.Join(root, "data")
	if err := Restore(archive, data); err == nil {
		t.Fatal("Restore() error = nil, want a rejection")
	}
	if _, err := os.Stat(filepath.Join(root, "escaped.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("archive escaped the target directory: %v", err)
	}
}

// writeEvilArchive creates a tar.gz with one entry outside the target.
func writeEvilArchive(t *testing.T, path, name string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)

	content := []byte("pwned")
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

// covers listing archives newest first
func TestList(t *testing.T) {
	t.Parallel()

	if archives, err := List(filepath.Join(t.TempDir(), "missing")); err != nil || archives != nil {
		t.Errorf("List(missing) = %v, %v; want nil, nil", archives, err)
	}

	dir := t.TempDir()
	times := map[string]time.Time{
		"2026-11-01_10-00-00.tar.gz": time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
		"2026-11-02_10-00-00.tar.gz": time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
		"2026-11-03_10-00-00.tar.gz": time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC),
	}
	files := map[string]string{"notes.txt": "not an archive"}
	for name := range times {
		files[name] = "archive"
	}
	writeTree(t, dir, files)
	for name, stamp := range times {
		path := filepath.Join(dir, name)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	archives, err := List(dir)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(archives) != 3 {
		t.Fatalf("List() returned %d archives, want 3", len(archives))
	}
	for i, archive := range archives {
		if archive.Name == "notes.txt" {
			t.Error("List() included a non-archive")
		}
		if archive.Bytes == 0 || archive.Path == "" {
			t.Errorf("archive = %+v, want a size and a path", archive)
		}
		if i > 0 && archive.Modified.After(archives[i-1].Modified) {
			t.Errorf("archives are not newest first: %v before %v", archives[i-1].Name, archive.Name)
		}
	}
	if archives[0].Name != "2026-11-03_10-00-00.tar.gz" {
		t.Errorf("newest = %s, want the 2026-11-03 archive", archives[0].Name)
	}

	newest, ok, err := Newest(dir)
	if err != nil || !ok {
		t.Fatalf("Newest() = %v, %v, %v; want a time", newest, ok, err)
	}
	if !newest.Equal(times["2026-11-03_10-00-00.tar.gz"]) {
		t.Errorf("Newest() = %v, want the newest modification time", newest)
	}
}

// covers retention of the newest archives
func TestPrune(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"2026-11-01_10-00-00.tar.gz": "one",
		"2026-11-02_10-00-00.tar.gz": "two",
		"2026-11-03_10-00-00.tar.gz": "three",
		"notes.txt":                  "not an archive",
	})

	removed, err := Prune(dir, 2)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != "2026-11-01_10-00-00.tar.gz" {
		t.Errorf("removed = %v, want the oldest archive", removed)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) != 3 {
		t.Errorf("directory holds %v, want two archives and the notes file", names)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Errorf("Prune() removed a non-archive: %v", err)
	}
	if !strings.Contains(names[0], "2026-11-02") && !strings.Contains(names[1], "2026-11-02") {
		t.Errorf("directory holds %v, want the newest archives", names)
	}

	if _, err := Prune(dir, 0); err == nil {
		t.Error("Prune(0) error = nil, want a failure")
	}
	if removed, err := Prune(filepath.Join(dir, "missing"), 2); err != nil || removed != nil {
		t.Errorf("Prune(missing) = %v, %v; want nil, nil", removed, err)
	}
}
