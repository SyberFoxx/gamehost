// Package process supervises the dedicated server process.
package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"
)

// Spec describes one server launch.
type Spec struct {
	Path string
	Args []string
	Dir  string
	Out  io.Writer
}

// Result describes how a server process ended.
type Result struct {
	ExitCode int
	Stopped  bool
	Duration time.Duration
}

// Server is a running server process.
type Server struct {
	cmd     *exec.Cmd
	started time.Time
}

// Start launches the process in its own process group.
func Start(spec Spec) (*Server, error) {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdout = spec.Out
	cmd.Stderr = spec.Out
	// A dedicated process group lets us signal the server and its children together.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", spec.Path, err)
	}
	return &Server{cmd: cmd, started: time.Now()}, nil
}

// PID returns the process id.
func (s *Server) PID() int {
	return s.cmd.Process.Pid
}

// WaitContext waits for the process, stopping it gracefully when ctx ends.
func (s *Server) WaitContext(ctx context.Context, grace time.Duration) Result {
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()

	select {
	case err := <-done:
		return s.result(err, false)
	case <-ctx.Done():
	}

	// Ask the whole process group to stop, then escalate once the grace period ends.
	_ = syscall.Kill(-s.PID(), syscall.SIGTERM)

	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case err := <-done:
		return s.result(err, true)
	case <-timer.C:
		_ = syscall.Kill(-s.PID(), syscall.SIGKILL)
		return s.result(<-done, true)
	}
}

// result maps the wait error to an exit code.
func (s *Server) result(err error, stopped bool) Result {
	res := Result{Stopped: stopped, Duration: time.Since(s.started)}
	if err == nil {
		return res
	}

	res.ExitCode = -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		res.ExitCode = exitErr.ExitCode()
	}
	return res
}
