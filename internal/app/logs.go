package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/syberfoxx/gamehost/internal/logs"
)

// runLogs prints or follows the newest log of one instance.
func runLogs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("logs", stderr)
	follow := fs.Bool("f", false, "follow the log until Ctrl+C")
	lines := fs.Int("n", 100, "lines to print from the end")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("logs needs exactly one instance name: %w", ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	inst, err := s.Load(fs.Arg(0))
	if err != nil {
		return err
	}

	path, err := logs.Latest(s.Layout(inst.Name).Logs)
	if errors.Is(err, logs.ErrNoLogs) {
		fmt.Fprintf(stdout, "no logs yet for %s, start it first\n", inst.Name)
		return nil
	}
	if err != nil {
		return err
	}

	if *follow {
		return logs.Follow(ctx, stdout, path, *lines)
	}
	return logs.Tail(stdout, path, *lines)
}
