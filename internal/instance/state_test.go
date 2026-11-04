package instance

import "testing"

// covers phase reconciliation against process liveness
func TestStateResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state State
		alive bool
		want  Phase
	}{
		{name: "running with a live process", state: State{Phase: PhaseRunning}, alive: true, want: PhaseRunning},
		{name: "running without a process", state: State{Phase: PhaseRunning}, want: PhaseCrashed},
		{name: "starting without a process", state: State{Phase: PhaseStarting}, want: PhaseCrashed},
		{name: "stopping after the process exited", state: State{Phase: PhaseStopping}, want: PhaseStopped},
		{name: "stopping while the process saves", state: State{Phase: PhaseStopping}, alive: true, want: PhaseStopping},
		{name: "stopped stays stopped", state: State{Phase: PhaseStopped}, want: PhaseStopped},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.state.Resolve(tt.alive); got.Phase != tt.want {
				t.Errorf("Resolve(%v) = %s, want %s", tt.alive, got.Phase, tt.want)
			}
		})
	}
}
