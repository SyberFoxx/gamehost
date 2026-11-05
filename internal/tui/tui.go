// Package tui renders the interactive lobby of gamehost.
//
// The package is presentation only: it imports nothing from the rest of the
// project. Everything it can do arrives through a Backend whose fields are
// functions, so the command layer stays the single owner of the behaviour and
// tests pass literals instead of implementing a fake interface.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
)

// DefaultLogLines is how much of the newest log the lobby shows at once.
const DefaultLogLines = 100

// Item is one instance, already resolved for display.
type Item struct {
	Name      string
	Game      string
	Phase     string
	PID       int
	StartedAt time.Time
	JoinLabel string
	JoinValue string
}

// Backend is what the lobby needs from the command layer.
//
// Every function blocks, so the model calls each one from a command goroutine
// and the lobby keeps drawing while SteamCMD or the server boots.
type Backend struct {
	List   func() ([]Item, error)
	Start  func(ctx context.Context, name string) (string, error)
	Stop   func(ctx context.Context, name string) (string, error)
	Backup func(ctx context.Context, name string) (string, error)
	Logs   func(name string, lines int) ([]string, error)
}

// validate reports the first callable the lobby would need and does not have.
func (b Backend) validate() error {
	if b.List == nil {
		return errors.New("tui: backend is missing List")
	}
	if b.Start == nil {
		return errors.New("tui: backend is missing Start")
	}
	if b.Stop == nil {
		return errors.New("tui: backend is missing Stop")
	}
	if b.Backup == nil {
		return errors.New("tui: backend is missing Backup")
	}
	if b.Logs == nil {
		return errors.New("tui: backend is missing Logs")
	}
	return nil
}

// Options configures one lobby run.
type Options struct {
	In      io.Reader
	Out     io.Writer
	Backend Backend
}

// Run renders the lobby until the user quits or ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	if opts.In == nil || opts.Out == nil {
		return errors.New("tui: input and output are required")
	}
	if err := opts.Backend.validate(); err != nil {
		return err
	}

	program := tea.NewProgram(newModel(ctx, opts.Backend),
		tea.WithContext(ctx),
		tea.WithInput(opts.In),
		tea.WithOutput(opts.Out),
	)

	// A cancelled context and Ctrl+C are a normal way to leave a lobby.
	if _, err := program.Run(); err != nil &&
		!errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return fmt.Errorf("run the lobby: %w", err)
	}
	return nil
}
