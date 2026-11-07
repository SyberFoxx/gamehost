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

// stopSignals ask gamehost to stop its server. A closed terminal sends SIGHUP;
// handling it turns closing the window into a clean stop instead of killing the
// process and leaving the server orphaned.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), stopSignals...)
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
