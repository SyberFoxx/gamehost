// Package process supervises the dedicated server process.
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// Spec describes one server launch.
type Spec struct {
	Path string
	Args []string
	Dir  string
	Out  io.Writer
	// In feeds the server's standard input. A driver with a console passes the
	// read end of its control file here.
	In io.Reader
	// Stop asks a console-driven server to leave before the signals run. A nil
	// Stop keeps the signal stop every game already uses.
	Stop func() error
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
	stop    func() error
}

const (
	// consoleGrace is how long a server gets to save and quit through its own
	// console before the process signals take over.
	consoleGrace = 30 * time.Second
	// stopPoll is the interval of every wait loop, so a stop returns promptly.
	stopPoll = 50 * time.Millisecond
)

// Start launches the process in its own process group.
func Start(spec Spec) (*Server, error) {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdout = spec.Out
	cmd.Stderr = spec.Out
	cmd.Stdin = spec.In
	// A dedicated process group lets us signal the server and its children together.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", spec.Path, err)
	}
	return &Server{cmd: cmd, started: time.Now(), stop: spec.Stop}, nil
}

// StartJoined launches a process in the process group of an existing server,
// so a single stop signal reaches every shard of one instance.
func StartJoined(spec Spec, pgid int) (*Server, error) {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdout = spec.Out
	cmd.Stderr = spec.Out
	cmd.Stdin = spec.In
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: pgid}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", spec.Path, err)
	}
	return &Server{cmd: cmd, started: time.Now(), stop: spec.Stop}, nil
}

// StartDetached launches a process in a new session, so it survives its parent.
// The caller owns the output writer and must keep it valid until the child starts.
func StartDetached(spec Spec) (int, error) {
	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Stdout = spec.Out
	cmd.Stderr = spec.Out
	// A new session detaches from the terminal: no controlling TTY, no SIGHUP
	// when the shell closes. Setsid already creates a process group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start detached %q: %w", spec.Path, err)
	}
	pid := cmd.Process.Pid
	// Release stops Go from waiting on a process that now belongs to init.
	if err := cmd.Process.Release(); err != nil {
		return 0, fmt.Errorf("release detached %q: %w", spec.Path, err)
	}
	return pid, nil
}

// Alive reports whether a process is running and can be signalled.
// A zombie answers signal 0 but will never run again, so it reports false.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if state, err := processState(pid); err == nil {
		return state != 'Z'
	}
	err := syscall.Kill(pid, 0)
	// EPERM means the process exists but belongs to another user.
	return err == nil || errors.Is(err, syscall.EPERM)
}

// processState reads the single-letter state from /proc/<pid>/stat.
// The process name may contain spaces and parentheses, so parse from the last ')'.
func processState(pid int) (byte, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+2 >= len(data) {
		return 0, fmt.Errorf("parse state of pid %d", pid)
	}
	return data[end+2], nil
}

// GroupAlive reports whether any process of the group still exists. A group
// outlives its leader, which is what keeps stop honest for an instance with
// more than one shard.
func GroupAlive(pgid int) bool {
	if pgid <= 0 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Stop asks a process group to exit and escalates to SIGKILL after grace.
func Stop(pid int, grace time.Duration) error {
	if pid <= 0 {
		return fmt.Errorf("stop: invalid pid %d", pid)
	}
	if err := terminate(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("stop pid %d: %w", pid, err)
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !Alive(pid) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if Alive(pid) {
		_ = terminate(pid, syscall.SIGKILL)
	}
	return nil
}

// terminate signals the process group, falling back to the process itself.
func terminate(pid int, sig syscall.Signal) error {
	// The negative pid targets every process the server spawned.
	err := syscall.Kill(-pid, sig)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return nil
	}
	if !errors.Is(err, syscall.ESRCH) {
		return err
	}
	// The group is already gone but the leader may still exist.
	if err := syscall.Kill(pid, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
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

	s.Stop(grace)
	return s.result(<-done, true)
}

// Stop asks the server to leave, first through its console when it has one and
// then through the process group signals, and returns once it is gone or the
// escalation ran out of time. The caller still owns the exit result.
func (s *Server) Stop(grace time.Duration) {
	if s.stop != nil {
		// A console that refuses the command is not fatal: the signals below
		// still end the server, which is what keeps a hung server stoppable.
		if err := s.stop(); err == nil && waitGone(s.PID(), consoleGrace) {
			return
		}
	}

	_ = terminate(s.PID(), syscall.SIGTERM)
	if waitGone(s.PID(), grace) {
		return
	}
	_ = terminate(s.PID(), syscall.SIGKILL)
}

// waitGone polls a process until it leaves or the window closes.
func waitGone(pid int, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if !Alive(pid) {
			return true
		}
		time.Sleep(stopPoll)
	}
	return !Alive(pid)
}

// result maps the wait error to an exit code.
func (s *Server) result(err error, stopped bool) Result {
	res := Result{Stopped: stopped, Duration: time.Since(s.started)}
	if err == nil {
		return res
	}

	res.ExitCode = -1
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return res
	}
	res.ExitCode = exitErr.ExitCode()

	// A signal from outside means someone asked the server to stop, which is a
	// clean stop even though it did not come from our context.
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		switch status.Signal() {
		case syscall.SIGTERM, syscall.SIGINT:
			res.Stopped = true
		}
	}
	return res
}
