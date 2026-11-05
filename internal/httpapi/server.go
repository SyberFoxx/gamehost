// Package httpapi exposes the instance lifecycle and log stream over HTTP.
package httpapi

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/syberfoxx/gamehost/internal/backup"
	"github.com/syberfoxx/gamehost/internal/games"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/logs"
	"github.com/syberfoxx/gamehost/internal/process"
	"github.com/syberfoxx/gamehost/internal/store"
)

// panel is the single-page UI served at the root.
//
//go:embed ui/index.html
var panel []byte

// defaultTail is how many lines the log stream replays on connect.
const defaultTail = 100

// Status is the API view of one instance and its runtime state.
type Status struct {
	Name       string         `json:"name"`
	Game       string         `json:"game"`
	Phase      instance.Phase `json:"phase"`
	PID        int            `json:"pid,omitempty"`
	Uptime     string         `json:"uptime,omitempty"`
	Join       string         `json:"join,omitempty"`
	LastExit   int            `json:"last_exit,omitempty"`
	World      string         `json:"world"`
	WorldMode  instance.Mode  `json:"world_mode"`
	MaxPlayers int            `json:"max_players"`
	Network    string         `json:"network"`
	Port       int            `json:"port,omitempty"`
}

// Controller runs the lifecycle operations the API exposes.
type Controller interface {
	Start(name string) error
	Stop(name string) error
}

// Maintenance archives and restores the data of one instance.
type Maintenance interface {
	Backup(name string) (backup.Result, error)
	Restore(name, archive string) error
}

// Server serves the API and the embedded panel.
type Server struct {
	store       *store.Store
	controller  Controller
	maintenance Maintenance
	mux         *http.ServeMux
}

// New returns a server for the instances in s, delegating lifecycle and data
// actions to the adapters so the API and the CLI share one implementation.
func New(s *store.Store, control Controller, maint Maintenance) *Server {
	srv := &Server{store: s, controller: control, maintenance: maint, mux: http.NewServeMux()}
	srv.mux.HandleFunc("GET /api/instances", srv.listInstances)
	srv.mux.HandleFunc("GET /api/instances/{name}", srv.showInstance)
	srv.mux.HandleFunc("POST /api/instances/{name}/start", srv.startInstance)
	srv.mux.HandleFunc("POST /api/instances/{name}/stop", srv.stopInstance)
	srv.mux.HandleFunc("GET /api/instances/{name}/logs", srv.followLogs)
	srv.mux.HandleFunc("GET /api/instances/{name}/backups", srv.listBackups)
	srv.mux.HandleFunc("POST /api/instances/{name}/backup", srv.createBackup)
	srv.mux.HandleFunc("POST /api/instances/{name}/restore", srv.restoreBackup)
	srv.mux.HandleFunc("GET /", srv.panel)
	return srv
}

// ServeHTTP routes one request to its handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// The panel can start, stop and restore instances, so a mutation must come
	// from the page this server served, never from another site open in the
	// same browser.
	if r.Method != http.MethodGet && r.Method != http.MethodHead && !s.sameOrigin(r) {
		writeError(w, http.StatusForbidden, "cross-site requests are not allowed")
		return
	}
	s.mux.ServeHTTP(w, r)
}

// sameOrigin reports whether a request may change state. A client without
// browser headers, such as curl, is trusted: it cannot be driven by a web page.
func (s *Server) sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// listInstances returns every instance with its resolved phase.
func (s *Server) listInstances(w http.ResponseWriter, _ *http.Request) {
	instances, err := s.store.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	statuses := make([]Status, 0, len(instances))
	for _, inst := range instances {
		status, err := s.status(inst)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		statuses = append(statuses, status)
	}
	writeJSON(w, http.StatusOK, statuses)
}

// showInstance returns one instance with its resolved phase.
func (s *Server) showInstance(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.lookup(w, r)
	if !ok {
		return
	}
	status, err := s.status(inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// startInstance launches a server in the background.
func (s *Server) startInstance(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.lookup(w, r)
	if !ok {
		return
	}
	state, err := s.store.LoadState(inst.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if state.Running() && process.Alive(state.PID) {
		writeError(w, http.StatusConflict, fmt.Sprintf("instance %q is already running (pid %d)", inst.Name, state.PID))
		return
	}

	if err := s.controller.Start(inst.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeStatus(w, inst)
}

// stopInstance asks a running server to exit and reports the final phase.
func (s *Server) stopInstance(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if err := s.controller.Stop(inst.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeStatus(w, inst)
}

// BackupResponse describes a fresh archive.
type BackupResponse struct {
	Archive string `json:"archive"`
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
}

// listBackups returns the archives of one instance, newest first.
func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.lookup(w, r)
	if !ok {
		return
	}
	archives, err := backup.List(s.store.Layout(inst.Name).Backups)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if archives == nil {
		archives = []backup.Archive{}
	}
	writeJSON(w, http.StatusOK, archives)
}

// createBackup archives the data of a stopped instance.
func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if s.refuseRunning(w, inst) {
		return
	}

	res, err := s.maintenance.Backup(inst.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, BackupResponse{
		Archive: filepath.Base(res.Path),
		Files:   res.Files,
		Bytes:   res.Bytes,
	})
}

// restoreBackup replaces the data of a stopped instance from one archive.
func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.lookup(w, r)
	if !ok {
		return
	}
	if s.refuseRunning(w, inst) {
		return
	}

	var req struct {
		Archive string `json:"archive"`
	}
	body := http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body must be {\"archive\": \"<name>.tar.gz\"}")
		return
	}
	// Only a bare file name inside the backup directory is accepted.
	if req.Archive == "" || req.Archive != filepath.Base(req.Archive) {
		writeError(w, http.StatusBadRequest, "archive must be a file name from GET /backups")
		return
	}
	if _, err := os.Stat(filepath.Join(s.store.Layout(inst.Name).Backups, req.Archive)); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("archive %q not found", req.Archive))
		return
	}

	if err := s.maintenance.Restore(inst.Name, req.Archive); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeStatus(w, inst)
}

// refuseRunning answers 409 when the instance has a live server.
func (s *Server) refuseRunning(w http.ResponseWriter, inst instance.Instance) bool {
	state, err := s.store.LoadState(inst.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return true
	}
	if state.Running() && process.Alive(state.PID) {
		writeError(w, http.StatusConflict, fmt.Sprintf("instance %q is running, stop it first", inst.Name))
		return true
	}
	return false
}

// followLogs streams the newest log as server-sent events.
func (s *Server) followLogs(w http.ResponseWriter, r *http.Request) {
	inst, ok := s.lookup(w, r)
	if !ok {
		return
	}
	path, err := logs.Latest(s.store.Layout(inst.Name).Logs)
	if errors.Is(err, logs.ErrNoLogs) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("no logs yet for %q, start it first", inst.Name))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "this server cannot stream responses")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	stream := &eventStream{w: w, flush: flusher}
	// The request context ends when the browser closes the stream.
	if err := logs.Follow(r.Context(), stream, path, tailParam(r)); err != nil {
		return
	}
}

// panel serves the embedded UI and rejects unknown paths.
func (s *Server) panel(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(panel) // the body is static, a write failure closes the connection
}

// writeStatus responds with the current status of inst.
func (s *Server) writeStatus(w http.ResponseWriter, inst instance.Instance) {
	status, err := s.status(inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// lookup validates the name in the path and loads its instance.
func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (instance.Instance, bool) {
	name := r.PathValue("name")
	if err := instance.ValidateName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return instance.Instance{}, false
	}
	inst, err := s.store.Load(name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return instance.Instance{}, false
	}
	return inst, true
}

// status combines the stored state with the liveness of its process.
func (s *Server) status(inst instance.Instance) (Status, error) {
	state, err := s.store.LoadState(inst.Name)
	if err != nil {
		return Status{}, err
	}
	live := process.Alive(state.PID)
	state = state.Resolve(live)

	status := Status{
		Name:       inst.Name,
		Game:       inst.Game,
		Phase:      state.Phase,
		World:      inst.World.Name,
		WorldMode:  inst.World.Mode,
		MaxPlayers: inst.MaxPlayers,
		Network:    inst.Network.Mode,
		Port:       inst.Network.Port,
	}
	if state.Running() {
		status.PID = state.PID
		status.Uptime = time.Since(state.StartedAt).Round(time.Second).String()
	}
	if state.Phase == instance.PhaseCrashed {
		status.LastExit = state.ExitCode
	}

	// The live value wins over the state, which only knows the last run.
	if driver, err := games.Lookup(inst.Game); err == nil {
		if _, value := driver.Runner.Join(inst); value != "" {
			status.Join = value
		} else {
			status.Join = state.GameID
		}
	}
	return status, nil
}

// tailParam reads the number of replayed log lines.
func tailParam(r *http.Request) int {
	raw := r.URL.Query().Get("tail")
	if raw == "" {
		return defaultTail
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultTail
	}
	return n
}

// eventStream turns written log lines into server-sent events.
type eventStream struct {
	w     io.Writer
	flush http.Flusher
	buf   []byte
}

// Write buffers bytes and emits one event per complete line.
func (e *eventStream) Write(p []byte) (int, error) {
	e.buf = append(e.buf, p...)
	for {
		end := bytes.IndexByte(e.buf, '\n')
		if end < 0 {
			return len(p), nil
		}
		line := bytes.TrimSuffix(e.buf[:end], []byte("\r"))
		if _, err := fmt.Fprintf(e.w, "data: %s\n\n", line); err != nil {
			return 0, err
		}
		e.flush.Flush()
		e.buf = e.buf[end+1:]
	}
}

// writeJSON encodes value with the given status.
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value) // the header is already sent, nothing to report
}

// writeError sends a JSON error message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
