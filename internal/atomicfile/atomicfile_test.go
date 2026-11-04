package atomicfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// covers the streaming path used to import a world
func TestReplaceFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "0.world.gzip")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ReplaceFile(path, bytes.NewBufferString("world bytes"), 0o600); err != nil {
		t.Fatalf("ReplaceFile() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "world bytes" {
		t.Errorf("file = %q, want the streamed content", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("perm = %v, want 0600", got)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only the target file", len(entries))
	}
}

// covers creating and overwriting a file without leftover temp copies
func TestReplace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		initial string
		content string
	}{
		{name: "creates a missing file", content: "first"},
		{name: "overwrites an existing file", initial: "old", content: "second"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "state.json")
			if tt.initial != "" {
				if err := os.WriteFile(path, []byte(tt.initial), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			if err := Replace(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("Replace() error = %v", err)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tt.content {
				t.Errorf("file = %q, want %q", data, tt.content)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Errorf("perm = %v, want 0600", got)
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Errorf("directory holds %d entries, want only the target file", len(entries))
			}
		})
	}
}
