package app

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// servePanel listens on addr and serves the browser panel with the same
// handler `serve` uses. The returned stop function shuts the listener down, and
// the lobby context does it too when the user quits without closing the screen.
func servePanel(ctx context.Context, addr string) (string, func(), error) {
	s, err := openStore()
	if err != nil {
		return "", nil, err
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, fmt.Errorf("listen on %s: %w", addr, err)
	}

	server := newPanelServer(s)
	go func() { _ = server.Serve(listener) }()

	stopped := make(chan struct{})
	var once sync.Once
	shutdown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close() // a stream that outlives the grace period is cut here
		}
	}
	stop := func() {
		once.Do(func() {
			close(stopped)
			shutdown()
		})
	}

	go func() {
		select {
		case <-ctx.Done():
		case <-stopped:
			return
		}
		shutdown()
	}()

	return listener.Addr().String(), stop, nil
}
