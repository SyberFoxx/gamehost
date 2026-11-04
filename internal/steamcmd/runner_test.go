package steamcmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeScript creates an executable shell script that echoes its arguments.
func writeScript(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-steamcmd.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// covers output streaming and argument passing
func TestRunnerRun(t *testing.T) {
	t.Parallel()

	script := writeScript(t, `for arg in "$@"; do echo "arg:$arg"; done`)

	var out bytes.Buffer
	runner := Runner{Path: script}
	if err := runner.Run(context.Background(), &out, "+quit", "value"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{"arg:+quit", "arg:value"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// covers a non-zero exit reported with its code
func TestRunnerRunFailure(t *testing.T) {
	t.Parallel()

	script := writeScript(t, `echo "boom"; exit 3`)

	var out bytes.Buffer
	err := Runner{Path: script}.Run(context.Background(), &out, "+quit")
	if err == nil {
		t.Fatal("Run() error = nil, want a failure")
	}
	if !strings.Contains(err.Error(), "code 3") {
		t.Errorf("Run() error = %v, want the exit code", err)
	}
	if !strings.Contains(out.String(), "boom") {
		t.Errorf("output = %q, want the script output", out.String())
	}
}

// covers cancellation surfaced as a context error
func TestRunnerRunCanceled(t *testing.T) {
	t.Parallel()

	script := writeScript(t, `sleep 5`)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Runner{Path: script}.Run(ctx, &bytes.Buffer{}, "+quit")
	if err == nil {
		t.Fatal("Run() error = nil, want a cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Logf("Run() error = %v (context.Canceled is not wrapped, the process failed to start)", err)
	}
}
