package instance

import "time"

// Phase is the lifecycle phase of a server.
type Phase string

// Lifecycle phases persisted between commands.
const (
	PhaseStopped  Phase = "stopped"
	PhaseStarting Phase = "starting"
	PhaseRunning  Phase = "running"
	PhaseStopping Phase = "stopping"
	PhaseCrashed  Phase = "crashed"
)

// State is the runtime state of one instance, separate from its configuration.
type State struct {
	Phase     Phase     `json:"phase"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	ExitCode  int       `json:"exit_code"`
	GameID    string    `json:"game_id"`
}

// Running reports whether the phase implies a live process.
func (s State) Running() bool {
	return s.Phase == PhaseStarting || s.Phase == PhaseRunning
}

// Resolve reconciles the stored phase with the liveness of its process.
// A running phase with a dead process means the server did not shut down
// cleanly; a stopping phase whose process is gone finished its stop.
func (s State) Resolve(alive bool) State {
	switch {
	case s.Running() && !alive:
		s.Phase = PhaseCrashed
	case s.Phase == PhaseStopping && !alive:
		s.Phase = PhaseStopped
	}
	return s
}
