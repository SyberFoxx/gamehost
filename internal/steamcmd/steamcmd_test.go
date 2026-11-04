package steamcmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// covers resolution order: override, PATH, user-local candidates
func TestLocate(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Parallel()

		script := filepath.Join(t.TempDir(), "steamcmd.sh")
		if err := os.WriteFile(script, nil, 0o755); err != nil {
			t.Fatal(err)
		}

		got, err := locate(
			func(string) (string, error) { return "/usr/bin/steamcmd", nil },
			"/home/test",
			func(string) string { return script },
		)
		if err != nil {
			t.Fatalf("locate() error = %v", err)
		}
		if got != script {
			t.Errorf("locate() = %q, want %q", got, script)
		}
	})

	t.Run("env override pointing nowhere", func(t *testing.T) {
		t.Parallel()

		_, err := locate(
			func(string) (string, error) { return "/usr/bin/steamcmd", nil },
			"/home/test",
			func(string) string { return "/does/not/exist" },
		)
		if err == nil {
			t.Fatal("locate() error = nil, want failure for a missing override")
		}
	})

	t.Run("path wins over user-local", func(t *testing.T) {
		t.Parallel()

		got, err := locate(
			func(string) (string, error) { return "/usr/bin/steamcmd", nil },
			"/home/test",
			func(string) string { return "" },
		)
		if err != nil {
			t.Fatalf("locate() error = %v", err)
		}
		if got != "/usr/bin/steamcmd" {
			t.Errorf("locate() = %q, want the PATH result", got)
		}
	})

	t.Run("user-local install", func(t *testing.T) {
		t.Parallel()

		homeDir := t.TempDir()
		script := filepath.Join(homeDir, ".local", "share", "steamcmd", "steamcmd.sh")
		if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(script, nil, 0o755); err != nil {
			t.Fatal(err)
		}

		got, err := locate(
			func(string) (string, error) { return "", errors.New("not found") },
			homeDir,
			func(string) string { return "" },
		)
		if err != nil {
			t.Fatalf("locate() error = %v", err)
		}
		if got != script {
			t.Errorf("locate() = %q, want %q", got, script)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		_, err := locate(
			func(string) (string, error) { return "", errors.New("not found") },
			t.TempDir(),
			func(string) string { return "" },
		)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("locate() error = %v, want ErrNotFound", err)
		}
	})
}
