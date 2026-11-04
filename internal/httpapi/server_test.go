package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// fakeController records lifecycle calls and returns a fixed error.
type fakeController struct {
	started []string
	stopped []string
	err     error
}

func (f *fakeController) Start(name string) error {
	f.started = append(f.started, name)
	return f.err
}

func (f *fakeController) Stop(name string) error {
	f.stopped = append(f.stopped, name)
	return f.err
}

// newTestServer returns a server with one instance and no state file.
func newTestServer(t *testing.T) (*Server, *store.Store, *fakeController) {
	t.Helper()

	s := store.New(t.TempDir())
	layout := s.Layout("corekeeper")
	inst, err := instance.New("corekeeper", instance.Options{
		MaxPlayers: 5,
		Mode:       instance.ModeNormal,
		InstallDir: layout.Files,
		DataDir:    layout.Data,
	})
	if err != nil {
		t.Fatalf("instance.New() error = %v", err)
	}
	if err := s.Create(inst); err != nil {
		t.Fatalf("store.Create() error = %v", err)
	}

	control := &fakeController{}
	return New(s, control), s, control
}

// decode reads a JSON response body into T.
func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var got T
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return got
}

// covers routing, status reporting and the lifecycle endpoints
func TestServerRoutes(t *testing.T) {
	t.Parallel()

	lifecycleErr := errors.New("lifecycle failed")
	tests := []struct {
		name       string
		method     string
		path       string
		controlErr error
		wantStatus int
		check      func(t *testing.T, rec *httptest.ResponseRecorder, control *fakeController)
	}{
		{
			name:       "lists instances",
			method:     http.MethodGet,
			path:       "/api/instances",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder, _ *fakeController) {
				got := decode[[]Status](t, rec)
				if len(got) != 1 || got[0].Name != "corekeeper" || got[0].Phase != instance.PhaseStopped {
					t.Errorf("instances = %+v, want one stopped corekeeper", got)
				}
			},
		},
		{
			name:       "shows one instance",
			method:     http.MethodGet,
			path:       "/api/instances/corekeeper",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder, _ *fakeController) {
				got := decode[Status](t, rec)
				if got.Name != "corekeeper" || got.MaxPlayers != 5 || got.Network != instance.NetworkSDR {
					t.Errorf("status = %+v, want the created instance over the relay", got)
				}
			},
		},
		{
			name:       "reports an unknown instance",
			method:     http.MethodGet,
			path:       "/api/instances/ghost",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "rejects an invalid name",
			method:     http.MethodGet,
			path:       "/api/instances/Bad%20Name",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "starts an instance",
			method:     http.MethodPost,
			path:       "/api/instances/corekeeper/start",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, _ *httptest.ResponseRecorder, control *fakeController) {
				if len(control.started) != 1 || control.started[0] != "corekeeper" {
					t.Errorf("started = %v, want corekeeper", control.started)
				}
			},
		},
		{
			name:       "stops an instance",
			method:     http.MethodPost,
			path:       "/api/instances/corekeeper/stop",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, _ *httptest.ResponseRecorder, control *fakeController) {
				if len(control.stopped) != 1 || control.stopped[0] != "corekeeper" {
					t.Errorf("stopped = %v, want corekeeper", control.stopped)
				}
			},
		},
		{
			name:       "reports a failed start",
			method:     http.MethodPost,
			path:       "/api/instances/corekeeper/start",
			controlErr: lifecycleErr,
			wantStatus: http.StatusInternalServerError,
			check: func(t *testing.T, rec *httptest.ResponseRecorder, control *fakeController) {
				if len(control.started) != 1 {
					t.Errorf("started = %v, want one attempt", control.started)
				}
				if got := decode[map[string]string](t, rec)["error"]; !strings.Contains(got, "lifecycle failed") {
					t.Errorf("error = %q, want the controller failure", got)
				}
			},
		},
		{
			name:       "reports missing logs",
			method:     http.MethodGet,
			path:       "/api/instances/corekeeper/logs",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "serves the panel",
			method:     http.MethodGet,
			path:       "/",
			wantStatus: http.StatusOK,
			check: func(t *testing.T, rec *httptest.ResponseRecorder, _ *fakeController) {
				if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
					t.Errorf("Content-Type = %q, want text/html", got)
				}
				if !strings.Contains(rec.Body.String(), "gamehost") {
					t.Error("panel body does not mention gamehost")
				}
			},
		},
		{
			name:       "rejects unknown paths",
			method:     http.MethodGet,
			path:       "/nope",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv, _, control := newTestServer(t)
			control.err = tt.controlErr

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.check != nil {
				tt.check(t, rec, control)
			}
		})
	}
}

// covers the event stream replaying the current tail
func TestLogStream(t *testing.T) {
	t.Parallel()

	srv, s, _ := newTestServer(t)
	logPath := filepath.Join(s.Layout("corekeeper").Logs, "2026-11-04_12-00-00.log")
	if err := os.WriteFile(logPath, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/instances/corekeeper/logs?tail=10", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("request the log stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	reader := bufio.NewReader(resp.Body)
	for _, want := range []string{"data: hello", "data: world"} {
		if got := nextEvent(t, reader); got != want {
			t.Errorf("event = %q, want %q", got, want)
		}
	}

	cancel()
}

// nextEvent reads the next non-empty line of the stream.
func nextEvent(t *testing.T, reader *bufio.Reader) string {
	t.Helper()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
}
