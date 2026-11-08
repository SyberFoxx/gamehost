package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/syberfoxx/gamehost/internal/backup"
	"github.com/syberfoxx/gamehost/internal/httpapi"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runServe serves the HTTP API and the embedded panel until ctx is cancelled.
func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("serve", stderr)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	var boot []string
	fs.Func("start", "instance to start when serve boots (repeatable)", func(value string) error {
		boot = append(boot, value)
		return nil
	})
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("serve takes no arguments: %w", ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	// A reboot leaves every instance stopped; --start gives the daemon that
	// first push so one unit can bring the server back on boot.
	startBootInstances(s, boot, stdout)
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}

	server := newPanelServer(s)
	fmt.Fprintf(stdout, "serving http://%s\npress Ctrl+C to stop\n", listener.Addr())

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	// Let in-flight requests finish, then drop the log streams still open.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close() // a stream that outlives the grace period is cut here
	}
	return nil
}

// newPanelServer builds the HTTP server the panel and serve share.
func newPanelServer(s *store.Store) *http.Server {
	return &http.Server{
		Handler:           httpapi.New(s, apiControl{}, apiMaintenance{}),
		ReadHeaderTimeout: 10 * time.Second,
	}
}

// startBootInstances starts every instance named on the serve command line.
// A failure is reported and never stops the daemon: the panel and the backup
// schedule must come up even when one world refuses to start.
func startBootInstances(s *store.Store, names []string, out io.Writer) {
	for _, name := range names {
		inst, err := s.Load(name)
		if err != nil {
			fmt.Fprintf(out, "boot start of %q: %v\n", name, err)
			continue
		}
		if err := startInstance(s, inst, out); err != nil {
			if errors.Is(err, ErrRunning) {
				fmt.Fprintf(out, "%s is already running\n", name)
				continue
			}
			fmt.Fprintf(out, "boot start of %q: %v\n", name, err)
		}
	}
}

// startInstance starts one instance in the background and waits until it runs.
func startInstance(s *store.Store, inst instance.Instance, stdout io.Writer) error {
	_, _, logPath, err := prepareStart(s, inst)
	if err != nil {
		return err
	}
	return startDetached(s, inst, logPath, stdout)
}

// apiControl adapts the CLI lifecycle to the HTTP API.
type apiControl struct{}

// Start launches one instance in the background and waits until it runs.
func (apiControl) Start(name string) error {
	s, inst, err := loadInstance(name)
	if err != nil {
		return err
	}
	return startInstance(s, inst, io.Discard)
}

// Stop asks one instance to exit and waits for it.
func (apiControl) Stop(name string) error {
	s, inst, err := loadInstance(name)
	if err != nil {
		return err
	}
	return stopInstance(s, inst, io.Discard)
}

// apiMaintenance adapts the CLI backup and restore to the HTTP API.
type apiMaintenance struct{}

// Backup archives one instance and reports the new archive.
func (apiMaintenance) Backup(name string) (backup.Result, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return backup.Result{}, err
	}
	return backupInstance(s, inst, defaultBackupKeep, io.Discard)
}

// Restore replaces the data of one instance from one archive in its backup
// directory. Only a bare file name is accepted, never a path.
func (apiMaintenance) Restore(name, archive string) error {
	s, inst, err := loadInstance(name)
	if err != nil {
		return err
	}
	return restoreInstance(s, inst, archive, io.Discard)
}
