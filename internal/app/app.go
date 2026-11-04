// Package app dispatches the gamehost command line.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"gamehost/internal/doctor"
	"gamehost/internal/home"
)

// version is overridden at build time with -ldflags "-X gamehost/internal/app.version=...".
var version = "dev"

// ErrUsage reports an invalid command line.
var ErrUsage = errors.New("usage")

// ErrUnhealthy reports an environment that cannot run servers yet.
var ErrUnhealthy = errors.New("environment is not ready")

const usage = `usage: gamehost <command>

commands:
  doctor    check the environment
  version   print the gamehost version
`

// Run executes one command and returns its error.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ErrUsage
	}

	switch args[0] {
	case "doctor":
		return runDoctor(stdout)
	case "version":
		fmt.Fprintf(stdout, "gamehost %s\n", version)
		return nil
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return ErrUsage
	}
}

// runDoctor prints the environment report and fails when it is not ready.
func runDoctor(stdout io.Writer) error {
	root, err := home.Dir()
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}

	report := doctor.Run(root)
	if err := doctor.Format(stdout, report); err != nil {
		return fmt.Errorf("write doctor report: %w", err)
	}
	if !report.Healthy() {
		return ErrUnhealthy
	}
	return nil
}
