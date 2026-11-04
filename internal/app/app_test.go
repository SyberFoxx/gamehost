package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runArgs executes one command line and captures its output.
func runArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), args, &stdout, &stderr)
	return stdout.String(), err
}

// covers command dispatch and usage errors
func TestRun(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantOut string
		wantErr error
	}{
		{"version", []string{"version"}, "gamehost dev\n", nil},
		{"no args", nil, "", ErrUsage},
		{"unknown command", []string{"nope"}, "", ErrUsage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			err := Run(context.Background(), tt.args, &stdout, &stderr)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Run() error = %v, want %v", err, tt.wantErr)
			}
			if got := stdout.String(); got != tt.wantOut {
				t.Errorf("stdout = %q, want %q", got, tt.wantOut)
			}
			if tt.wantErr != nil && !strings.Contains(stderr.String(), "usage:") {
				t.Errorf("stderr = %q, want usage message", stderr.String())
			}
		})
	}
}

// covers the create, list and show round trip
func TestCreateListShow(t *testing.T) {
	// t.Setenv forbids t.Parallel.
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	out, err := runArgs(t, "create", "corekeeper", "--max-players", "5", "--world-name", "Cueva")
	if err != nil {
		t.Fatalf("create error = %v", err)
	}
	if !strings.Contains(out, `created instance "corekeeper"`) {
		t.Errorf("create output = %q", out)
	}

	out, err = runArgs(t, "list")
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	if !strings.Contains(out, "corekeeper") || !strings.Contains(out, "Cueva") {
		t.Errorf("list output = %q, want the created instance", out)
	}

	out, err = runArgs(t, "show", "corekeeper")
	if err != nil {
		t.Fatalf("show error = %v", err)
	}
	for _, want := range []string{"corekeeper", "Cueva", "sdr", "(generated on first start)"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}

	if _, err := runArgs(t, "create", "corekeeper"); !errors.Is(err, store.ErrExists) {
		t.Errorf("second create error = %v, want ErrExists", err)
	}
}

// covers rejected configurations
func TestCreateRejects(t *testing.T) {
	t.Setenv("GAMEHOST_HOME", t.TempDir())

	tests := []struct {
		name string
		args []string
		want error
	}{
		{"bad name", []string{"create", "../escape"}, instance.ErrInvalid},
		{"bad mode", []string{"create", "ok", "--mode", "nightmare"}, instance.ErrInvalid},
		{"too many players", []string{"create", "ok", "--max-players", "9"}, instance.ErrInvalid},
		{"missing name", []string{"create"}, ErrUsage},
		{"unknown flag", []string{"create", "ok", "--nope"}, ErrUsage},
		{"port without range", []string{"create", "ok", "--port", "70000"}, instance.ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := runArgs(t, tt.args...); !errors.Is(err, tt.want) {
				t.Errorf("%v error = %v, want %v", tt.args, err, tt.want)
			}
		})
	}
}
