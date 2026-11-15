package process

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

var (
	// ErrNoConsole reports a control file that nothing is reading, which means
	// the server is not up or was not launched with one.
	ErrNoConsole = errors.New("no server reads the control file")
	// ErrNotConsole reports a path that exists and is not a fifo.
	ErrNotConsole = errors.New("control path is not a fifo")
	// ErrInvalidCommand reports a line that would reach the server as more than
	// one command.
	ErrInvalidCommand = errors.New("invalid console command")
)

// Control is the fifo a server reads console commands from. The holder keeps it
// open for reading and writing, so opening it never blocks, the server never
// reads EOF between commands, and a writer never waits for a reader to appear.
type Control struct {
	path string
	file *os.File
}

// OpenControl creates the fifo when it is missing and takes the holder end.
// It refuses a path a game or a user already owns instead of replacing it.
func OpenControl(path string) (*Control, error) {
	info, err := os.Lstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeNamedPipe == 0 {
			return nil, fmt.Errorf("open control file %q: %w", path, ErrNotConsole)
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := syscall.Mkfifo(path, 0o660); err != nil {
			return nil, fmt.Errorf("create control file %q: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("stat %q: %w", path, err)
	}

	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open control file %q: %w", path, err)
	}
	return &Control{path: path, file: file}, nil
}

// File is the read end a launch hands to the server as its standard input.
func (c *Control) File() io.Reader {
	return c.file
}

// Path is the control file of this holder.
func (c *Control) Path() string {
	return c.path
}

// Send writes one line per command to the held control file.
func (c *Control) Send(lines ...string) error {
	return send(c.path, lines...)
}

// Close releases the holder. The next launch creates the fifo again.
func (c *Control) Close() error {
	return c.file.Close()
}

// SendControl writes to a control file another process holds, which is what a
// `stop` running in its own process does.
func SendControl(path string, lines ...string) error {
	return send(path, lines...)
}

// send writes the commands in one atomic write, so the server never reads half
// a line.
func send(path string, lines ...string) error {
	payload, err := commandPayload(lines)
	if err != nil {
		return err
	}

	// O_NONBLOCK makes an empty fifo answer ENXIO instead of waiting forever
	// for a reader that may never come.
	file, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		// A missing file and a fifo nobody reads mean the same thing to a
		// caller: there is no live console to talk to.
		if errors.Is(err, syscall.ENXIO) || errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("send to control file %q: %w", path, ErrNoConsole)
		}
		return fmt.Errorf("open control file %q: %w", path, err)
	}
	defer func() { _ = file.Close() }() // the write below is the only operation

	if _, err := file.Write(payload); err != nil {
		if errors.Is(err, syscall.ENXIO) || errors.Is(err, syscall.EPIPE) {
			return fmt.Errorf("send to control file %q: %w", path, ErrNoConsole)
		}
		return fmt.Errorf("write control file %q: %w", path, err)
	}
	return nil
}

// commandPayload joins the commands into the bytes one write sends.
func commandPayload(lines []string) ([]byte, error) {
	if len(lines) == 0 {
		return nil, fmt.Errorf("no console command: %w", ErrInvalidCommand)
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		command := strings.TrimSpace(line)
		if command == "" {
			return nil, fmt.Errorf("empty console command: %w", ErrInvalidCommand)
		}
		if strings.ContainsAny(command, "\r\n") {
			return nil, fmt.Errorf("console command %q has a line break: %w", command, ErrInvalidCommand)
		}
		out = append(out, command)
	}
	return []byte(strings.Join(out, "\n") + "\n"), nil
}
