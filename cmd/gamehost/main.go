// Command gamehost manages dedicated game servers from the terminal.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"gamehost/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, app.ErrUsage) {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "gamehost: %v\n", err)
		os.Exit(1)
	}
}
