package corekeeper

import (
	"slices"
	"testing"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// baseInstance returns a configured instance for argument tests.
func baseInstance(t *testing.T) instance.Instance {
	t.Helper()

	inst, err := instance.New("corekeeper", instance.Options{
		MaxPlayers: 5,
		WorldName:  "Cueva Compartida",
		Mode:       instance.ModeHard,
		InstallDir: "/srv/files",
		DataDir:    "/srv/data",
	})
	if err != nil {
		t.Fatalf("instance.New() error = %v", err)
	}
	return inst
}

// covers the relay-mode argument set
func TestArgsRelay(t *testing.T) {
	t.Parallel()

	want := []string{
		"-batchmode",
		"-logfile", "/dev/stdout",
		"-datapath", "/srv/data",
		"-world", "0",
		"-worldname", "Cueva Compartida",
		"-worldmode", "Hard",
		"-maxplayers", "5",
	}

	got, err := Args(baseInstance(t))
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("Args() = %v, want %v", got, want)
	}
}

// covers seed, fixed game id and direct connections
func TestArgsOptionalFlags(t *testing.T) {
	t.Parallel()

	inst, err := instance.New("corekeeper", instance.Options{
		MaxPlayers: 4,
		WorldName:  "World",
		Seed:       "nice seed",
		Mode:       instance.ModeCasual,
		Port:       27015,
		Password:   "hunter2",
		GameID:     "abcdef1234567891",
		InstallDir: "/srv/files",
		DataDir:    "/srv/data",
	})
	if err != nil {
		t.Fatalf("instance.New() error = %v", err)
	}

	got, err := Args(inst)
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}

	for _, want := range [][2]string{
		{"-worldseed", "nice seed"},
		{"-gameid", "abcdef1234567891"},
		{"-port", "27015"},
		{"-password", "hunter2"},
	} {
		if !hasPair(got, want[0], want[1]) {
			t.Errorf("Args() missing %s %s:\n%v", want[0], want[1], got)
		}
	}
}

// covers the relay-mode omission of port and password
func TestArgsRelayOmitsDirectFlags(t *testing.T) {
	t.Parallel()

	got, err := Args(baseInstance(t))
	if err != nil {
		t.Fatalf("Args() error = %v", err)
	}
	for _, unwanted := range []string{"-port", "-password"} {
		if slices.Contains(got, unwanted) {
			t.Errorf("Args() contains %q in relay mode: %v", unwanted, got)
		}
	}
}

// hasPair reports whether args contains flag followed by value.
func hasPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
