package process

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeScript creates an executable shell script.
func writeScript(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "server.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// covers a normal exit code
func TestWaitContextExitCode(t *testing.T) {

	server, err := Start(Spec{Path: writeScript(t, "exit 7"), Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if server.PID() <= 0 {
		t.Fatalf("PID() = %d, want a live process", server.PID())
	}

	res := server.WaitContext(context.Background(), time.Second)
	if res.ExitCode != 7 {
		t.Errorf("ExitCode = %d, want 7", res.ExitCode)
	}
	if res.Stopped {
		t.Error("Stopped = true, want false for a self-terminated process")
	}
}

// covers a graceful stop through the process group
func TestWaitContextGracefulStop(t *testing.T) {

	script := writeScript(t, "trap 'exit 0' TERM\nwhile :; do sleep 0.05; done")
	server, err := Start(Spec{Path: script, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	res := server.WaitContext(ctx, 2*time.Second)
	if !res.Stopped {
		t.Error("Stopped = false, want true")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 after a graceful stop", res.ExitCode)
	}
	if res.Duration > 2*time.Second {
		t.Errorf("Duration = %s, the stop did not happen", res.Duration)
	}
}

// covers the SIGKILL escalation when the process ignores SIGTERM
func TestWaitContextKillsStubbornProcess(t *testing.T) {

	script := writeScript(t, "trap '' TERM\nwhile :; do sleep 0.05; done")
	server, err := Start(Spec{Path: script, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := server.WaitContext(ctx, 200*time.Millisecond)
	if !res.Stopped {
		t.Error("Stopped = false, want true")
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1 for a killed process", res.ExitCode)
	}
}

// covers the missing binary path
func TestStartMissingBinary(t *testing.T) {

	if _, err := Start(Spec{Path: filepath.Join(t.TempDir(), "nope"), Out: &bytes.Buffer{}}); err == nil {
		t.Fatal("Start() error = nil, want a failure")
	}
}

// covers an external SIGTERM counting as a clean stop
func TestWaitContextExternalTermIsStopped(t *testing.T) {
	script := writeScript(t, "trap 'exit 0' TERM\nwhile :; do sleep 0.05; done")
	server, err := Start(Spec{Path: script, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := Stop(server.PID(), 300*time.Millisecond); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	res := server.WaitContext(context.Background(), time.Second)
	if !res.Stopped {
		t.Error("Stopped = false, want true for a signal from outside")
	}
}

// covers stopping a process group and the missing pid cases
func TestStop(t *testing.T) {
	script := writeScript(t, "trap 'exit 0' TERM\nwhile :; do sleep 0.05; done")
	server, err := Start(Spec{Path: script, Out: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := Stop(server.PID(), 300*time.Millisecond); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	// The parent must reap the child, otherwise it stays visible as a zombie.
	_ = server.WaitContext(context.Background(), time.Second)
	if Alive(server.PID()) {
		t.Error("Alive() = true after Stop")
	}
	if err := Stop(server.PID(), time.Second); err != nil {
		t.Errorf("second Stop() error = %v, want nil for a dead process", err)
	}
	if err := Stop(0, time.Second); err == nil {
		t.Error("Stop(0) error = nil, want a failure")
	}
}
