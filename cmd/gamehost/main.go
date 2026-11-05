// Command gamehost manages dedicated game servers from the terminal.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/syberfoxx/gamehost/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		switch {
		case errors.Is(err, app.ErrUsage):
			// A usage error still has to say what was wrong.
			fmt.Fprintf(os.Stderr, "gamehost: %v\n", err)
			os.Exit(2)
		case errors.Is(err, app.ErrUnhealthy):
			os.Exit(1)
		default:
			fmt.Fprintf(os.Stderr, "gamehost: %v\n", err)
			os.Exit(1)
		}
	}
}
