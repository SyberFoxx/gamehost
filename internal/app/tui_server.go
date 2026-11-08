package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/syberfoxx/gamehost/internal/backup"
	"github.com/syberfoxx/gamehost/internal/logs"
	"github.com/syberfoxx/gamehost/internal/tui"
)

// lobbyStart hands the instance to the detached supervisor, the same path
// start --detach uses. It blocks until the supervisor reports a pid.
func lobbyStart(_ context.Context, name string) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	_, _, logPath, err := prepareStart(s, inst)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := startDetached(s, inst, logPath, &out); err != nil {
		return "", err
	}
	return firstLine(out.String()), nil
}

// lobbyStop asks the server to save and exit.
func lobbyStop(_ context.Context, name string) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := stopInstance(s, inst, &out); err != nil {
		return "", err
	}
	return lastLine(out.String()), nil
}

// lobbyBackup archives the world of a stopped instance with the retention the
// backup form asked for, the same value `backup --keep` takes.
func lobbyBackup(_ context.Context, name string, keep int) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if _, err := backupInstance(s, inst, keep, &out); err != nil {
		return "", err
	}
	return firstLine(out.String()), nil
}

// lobbyRestore replaces the world from one archive of the backup directory.
func lobbyRestore(_ context.Context, name, archive string) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := restoreInstance(s, inst, archive, &out); err != nil {
		return "", err
	}
	return firstLine(out.String()), nil
}

// lobbyImportWorld copies one save into the instance's slot.
func lobbyImportWorld(_ context.Context, name, path string) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := importWorld(s, inst, path, &out); err != nil {
		return "", err
	}
	return firstLine(out.String()), nil
}

// lobbyBackups lists the archives of one instance, newest first.
func lobbyBackups(name string) ([]tui.Archive, error) {
	s, err := openStore()
	if err != nil {
		return nil, err
	}

	archives, err := backup.List(s.Layout(name).Backups)
	if err != nil {
		return nil, err
	}

	out := make([]tui.Archive, 0, len(archives))
	for _, archive := range archives {
		out = append(out, tui.Archive{
			Name:     archive.Name,
			Bytes:    archive.Bytes,
			Modified: archive.Modified,
		})
	}
	return out, nil
}

// lobbyDownload installs or updates the server files and streams the output.
func lobbyDownload(ctx context.Context, command, name string) <-chan tui.Event {
	events := make(chan tui.Event, 64)

	go func() {
		defer close(events)

		s, inst, err := loadInstance(name)
		if err != nil {
			sendEvent(ctx, events, tui.Event{Err: err, Done: true})
			return
		}

		writer := &eventWriter{ctx: ctx, events: events}
		err = installInstance(ctx, command, s, inst, writer)
		writer.flush()
		if err != nil {
			sendEvent(ctx, events, tui.Event{Err: err})
		}
		sendEvent(ctx, events, tui.Event{Done: true})
	}()

	return events
}

// lobbyFollow tails the newest log and follows it until ctx is cancelled.
func lobbyFollow(ctx context.Context, name string, lines int) <-chan tui.Event {
	events := make(chan tui.Event, 64)

	go func() {
		defer close(events)

		s, inst, err := loadInstance(name)
		if err != nil {
			sendEvent(ctx, events, tui.Event{Err: err, Done: true})
			return
		}

		path, err := logs.Latest(s.Layout(inst.Name).Logs)
		if errors.Is(err, logs.ErrNoLogs) {
			sendEvent(ctx, events, tui.Event{Line: fmt.Sprintf("no logs yet for %s, start it first", inst.Name)})
			sendEvent(ctx, events, tui.Event{Done: true})
			return
		}
		if err != nil {
			sendEvent(ctx, events, tui.Event{Err: err})
			sendEvent(ctx, events, tui.Event{Done: true})
			return
		}

		writer := &eventWriter{ctx: ctx, events: events}
		err = logs.Follow(ctx, writer, path, lines)
		writer.flush()
		if err != nil {
			sendEvent(ctx, events, tui.Event{Err: err})
		}
		sendEvent(ctx, events, tui.Event{Done: true})
	}()

	return events
}

// eventWriter turns an io.Writer into a stream of complete lines. The context
// guards every send, so a screen that stopped reading cannot leak the goroutine.
type eventWriter struct {
	ctx    context.Context
	events chan<- tui.Event

	mu      sync.Mutex
	pending string
}

// Write splits the chunk into lines and sends the complete ones.
func (w *eventWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending += string(p)
	for {
		index := strings.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		line := w.pending[:index]
		w.pending = w.pending[index+1:]
		if !sendEvent(w.ctx, w.events, tui.Event{Line: line}) {
			return len(p), nil
		}
	}
	return len(p), nil
}

// flush sends the last partial line, if any.
func (w *eventWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.pending == "" {
		return
	}
	sendEvent(w.ctx, w.events, tui.Event{Line: w.pending})
	w.pending = ""
}

// sendEvent writes one event or gives up when the consumer is gone.
func sendEvent(ctx context.Context, events chan<- tui.Event, event tui.Event) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
