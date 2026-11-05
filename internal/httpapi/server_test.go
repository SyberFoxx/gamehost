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

	"github.com/syberfoxx/gamehost/internal/backup"
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

// fakeMaintenance records archive calls and returns fixed values.
type fakeMaintenance struct {
	backedUp []string
	restored []string
	archive  backup.Result
	err      error
}

func (f *fakeMaintenance) Backup(name string) (backup.Result, error) {
	f.backedUp = append(f.backedUp, name)
	return f.archive, f.err
}

func (f *fakeMaintenance) Restore(name, archive string) error {
	f.restored = append(f.restored, name+"/"+archive)
	return f.err
}

// newTestServer returns a server with one instance and no state file.
func newTestServer(t *testing.T) (*Server, *store.Store, *fakeController, *fakeMaintenance) {
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
	maint := &fakeMaintenance{}
	return New(s, control, maint), s, control, maint
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

			srv, _, control, _ := newTestServer(t)
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

// covers listing, creating and restoring archives
func TestBackups(t *testing.T) {
	t.Parallel()

	srv, s, _, maint := newTestServer(t)
	name := "2026-11-04_10-00-00.tar.gz"
	maint.archive = backup.Result{Path: filepath.Join("backups", name), Files: 2, Bytes: 100}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/instances/corekeeper/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("empty list = %q, want []", got)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/instances/corekeeper/backup", nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("backup status = %d (body %q)", rec.Code, rec.Body.String())
	}
	created := decode[BackupResponse](t, rec)
	if created.Archive != name || created.Files != 2 || created.Bytes != 100 {
		t.Errorf("backup = %+v, want %s with 2 files", created, name)
	}
	if len(maint.backedUp) != 1 || maint.backedUp[0] != "corekeeper" {
		t.Errorf("backedUp = %v, want corekeeper", maint.backedUp)
	}

	archivePath := filepath.Join(s.Layout("corekeeper").Backups, name)
	if err := os.WriteFile(archivePath, []byte("archive"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/instances/corekeeper/backups", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if archives := decode[[]backup.Archive](t, rec); len(archives) != 1 || archives[0].Name != name {
		t.Errorf("archives = %+v, want %s", archives, name)
	}

	rec = httptest.NewRecorder()
	body := strings.NewReader(`{"archive":"` + name + `"}`)
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/instances/corekeeper/restore", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if len(maint.restored) != 1 || maint.restored[0] != "corekeeper/"+name {
		t.Errorf("restored = %v, want corekeeper/%s", maint.restored, name)
	}

	tests := []struct {
		name string
		body string
		want int
	}{
		{"rejects a path", `{"archive":"../world.tar.gz"}`, http.StatusBadRequest},
		{"rejects an empty archive", `{"archive":""}`, http.StatusBadRequest},
		{"reports a missing archive", `{"archive":"missing.tar.gz"}`, http.StatusNotFound},
		{"rejects a malformed body", `not json`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := strings.NewReader(tt.body)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/instances/corekeeper/restore", body))
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body %q)", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

// covers data endpoints refusing a live server
func TestBackupsRefuseRunningInstance(t *testing.T) {
	t.Parallel()

	srv, s, _, maint := newTestServer(t)
	if err := s.SaveState("corekeeper", instance.State{
		Phase: instance.PhaseRunning,
		PID:   os.Getpid(),
	}); err != nil {
		t.Fatal(err)
	}

	for _, action := range []string{"backup", "restore"} {
		rec := httptest.NewRecorder()
		body := strings.NewReader(`{"archive":"x.tar.gz"}`)
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/instances/corekeeper/"+action, body))
		if rec.Code != http.StatusConflict {
			t.Errorf("%s status = %d, want 409", action, rec.Code)
		}
	}
	if len(maint.backedUp) != 0 || len(maint.restored) != 0 {
		t.Errorf("maintenance ran while running: %v %v", maint.backedUp, maint.restored)
	}
}

// covers the event stream replaying the current tail
func TestLogStream(t *testing.T) {
	t.Parallel()

	srv, s, _, _ := newTestServer(t)
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

// covers rejecting cross-site state changes
func TestServeHTTPRejectsCrossSiteWrites(t *testing.T) {
	t.Parallel()

	srv, _, control, _ := newTestServer(t)

	post := func(origin, site string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/instances/corekeeper/start", nil)
		req.Host = "127.0.0.1:8080"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if site != "" {
			req.Header.Set("Sec-Fetch-Site", site)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}

	tests := []struct {
		name   string
		origin string
		site   string
		want   int
	}{
		{"same origin", "http://127.0.0.1:8080", "", http.StatusOK},
		{"no browser headers", "", "", http.StatusOK},
		{"another site", "http://evil.example", "", http.StatusForbidden},
		{"other port", "http://127.0.0.1:3000", "", http.StatusForbidden},
		{"opaque origin", "null", "", http.StatusForbidden},
		{"browser flagged cross-site", "http://127.0.0.1:8080", "cross-site", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := post(tt.origin, tt.site)
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body)
			}
		})
	}

	if len(control.started) != 2 {
		t.Errorf("started = %v, want only the two allowed requests", control.started)
	}
}

// covers reads from another origin staying allowed
func TestServeHTTPAllowsCrossSiteReads(t *testing.T) {
	t.Parallel()

	srv, _, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/instances", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 for a read", rec.Code)
	}
}
