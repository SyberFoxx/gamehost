package process

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// envConsoleServer marks the child process a console test launches.
const envConsoleServer = "GAMEHOST_TEST_CONSOLE_SERVER"

// TestConsoleServerHelper is the child of TestServerStopThroughConsole. It
// reads one console line and leaves with a code that proves how it ended.
func TestConsoleServerHelper(t *testing.T) {
	if os.Getenv(envConsoleServer) != "1" {
		t.Skip("helper process only")
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		os.Exit(2)
	}
	if strings.TrimSpace(line) != "quit" {
		os.Exit(3)
	}
	os.Exit(0)
}

// TestServerStopThroughConsole covers a launch that reads its own console: the
// clean command ends it, and no signal is needed to get there.
func TestServerStopThroughConsole(t *testing.T) {
	// The child inherits the environment, so this test cannot run in parallel.
	t.Setenv(envConsoleServer, "1")

	control, err := OpenControl(filepath.Join(t.TempDir(), "gamehost.control"))
	if err != nil {
		t.Fatalf("OpenControl() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = control.Close() })

	server, err := Start(Spec{
		Path: os.Args[0],
		Args: []string{"-test.run=TestConsoleServerHelper"},
		Out:  io.Discard,
		In:   control.File(),
		Stop: func() error { return control.Send("quit") },
	})
	if err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(200 * time.Millisecond)
		cancel()
	}()

	// An exit code of 0 is what the child writes only after reading `quit`; a
	// signal stop would report -1 and hide a broken console.
	res := server.WaitContext(ctx, 5*time.Second)
	if !res.Stopped {
		t.Fatalf("WaitContext() Stopped = false, want true")
	}
	if res.ExitCode != 0 {
		t.Fatalf("WaitContext() ExitCode = %d, want 0", res.ExitCode)
	}
}
