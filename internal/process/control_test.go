package process

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// readLines collects the lines a server would read from a control file.
func readLines(t *testing.T, r interface{ Read([]byte) (int, error) }) <-chan string {
	t.Helper()
	out := make(chan string, 8)
	go func() {
		defer close(out)
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			out <- scanner.Text()
		}
	}()
	return out
}

// nextLine waits for one line, failing the test when none arrives.
func nextLine(t *testing.T, lines <-chan string) string {
	t.Helper()
	select {
	case line, ok := <-lines:
		if !ok {
			t.Fatal("the control file closed before a line arrived")
		}
		return line
	case <-time.After(2 * time.Second):
		t.Fatal("no line arrived on the control file")
		return ""
	}
}

// TestOpenControlCreatesFIFO covers the file the server reads its commands from.
func TestOpenControlCreatesFIFO(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gamehost.control")
	ctl, err := OpenControl(path)
	if err != nil {
		t.Fatalf("OpenControl() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = ctl.Close() })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v, want nil", err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("control file mode = %v, want a named pipe", info.Mode())
	}

	// A second holder is what a supervisor restart and a concurrent `stop`
	// look like; opening again must not truncate the file or fail.
	again, err := OpenControl(path)
	if err != nil {
		t.Fatalf("second OpenControl() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = again.Close() })
}

// TestOpenControlRefusesRegularFile covers a path a game or a user already owns.
func TestOpenControlRefusesRegularFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gamehost.control")
	if err := os.WriteFile(path, []byte("not a pipe\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := OpenControl(path); err == nil {
		t.Fatal("OpenControl() error = nil, want a refusal")
	}
	// The file the user owns keeps its bytes.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "not a pipe\n" {
		t.Fatalf("control file = %q, want the original content", data)
	}
}

// TestControlSendAndRead covers the round trip a launch and `stop` rely on.
func TestControlSendAndRead(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gamehost.control")
	ctl, err := OpenControl(path)
	if err != nil {
		t.Fatalf("OpenControl() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = ctl.Close() })

	lines := readLines(t, ctl.File())
	if err := ctl.Send("save", "quit"); err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}
	if got := nextLine(t, lines); got != "save" {
		t.Fatalf("first line = %q, want %q", got, "save")
	}
	if got := nextLine(t, lines); got != "quit" {
		t.Fatalf("second line = %q, want %q", got, "quit")
	}
}

// TestSendControlFromAnotherProcess covers `gamehost stop`, which does not own
// the control file and only writes to it.
func TestSendControlFromAnotherProcess(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gamehost.control")
	ctl, err := OpenControl(path)
	if err != nil {
		t.Fatalf("OpenControl() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = ctl.Close() })

	lines := readLines(t, ctl.File())
	if err := SendControl(path, "save", "quit"); err != nil {
		t.Fatalf("SendControl() error = %v, want nil", err)
	}
	if got := nextLine(t, lines); got != "save" {
		t.Fatalf("first line = %q, want %q", got, "save")
	}
	if got := nextLine(t, lines); got != "quit" {
		t.Fatalf("second line = %q, want %q", got, "quit")
	}
}

// TestSendControlWithoutServer covers a control file nothing reads: the caller
// must be able to tell that from a write that worked.
func TestSendControlWithoutServer(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gamehost.control")
	if err := SendControl(path, "quit"); !errors.Is(err, ErrNoConsole) {
		t.Fatalf("SendControl() error = %v, want %v", err, ErrNoConsole)
	}
}

// TestSendControlRejectsSeveralCommandsInOneLine covers a command string that
// would smuggle a second command into the server.
func TestSendControlRejectsSeveralCommandsInOneLine(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "gamehost.control")
	ctl, err := OpenControl(path)
	if err != nil {
		t.Fatalf("OpenControl() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = ctl.Close() })

	if err := ctl.Send("save\nquit"); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("Send() error = %v, want %v", err, ErrInvalidCommand)
	}
}
