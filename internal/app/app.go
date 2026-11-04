// Package app dispatches the gamehost command line.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// version is overridden at build time with -ldflags "-X gamehost/internal/app.version=...".
var version = "dev"

// ErrUsage reports an invalid command line.
var ErrUsage = errors.New("usage")

const usage = `usage: gamehost <command>

commands:
  version   print the gamehost version
`

// Run executes one command and returns its error.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ErrUsage
	}

	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "gamehost %s\n", version)
		return nil
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return ErrUsage
	}
}
