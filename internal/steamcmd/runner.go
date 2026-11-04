package steamcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Runner executes the SteamCMD client.
type Runner struct {
	Path string
}

// Run executes SteamCMD, streaming its output to out.
func (r Runner) Run(ctx context.Context, out io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, r.Path, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	// Stop waiting on inherited pipes shortly after the process exits or the context ends.
	cmd.WaitDelay = 10 * time.Second

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("steamcmd exited with code %d: %w", exitErr.ExitCode(), err)
		}
		return fmt.Errorf("run %q: %w", r.Path, err)
	}
	return nil
}
