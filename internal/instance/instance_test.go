package instance

import (
	"errors"
	"strings"
	"testing"
)

// covers name validation
func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple", "corekeeper", false},
		{"digits and dashes", "world-2_beta", false},
		{"empty", "", true},
		{"uppercase", "CoreKeeper", true},
		{"path traversal", "../etc", true},
		{"leading dash", "-server", true},
		{"spaces", "my server", true},
		{"too long", strings.Repeat("a", 33), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateName(tt.input)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("ValidateName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalid) {
				t.Errorf("ValidateName(%q) error = %v, want ErrInvalid", tt.input, err)
			}
		})
	}
}

// covers defaults and derived network mode
func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		opts    Options
		wantErr bool
		assert  func(t *testing.T, got Instance)
	}{
		{
			name:  "defaults",
			input: "corekeeper",
			opts: Options{
				MaxPlayers: 5,
				Mode:       ModeNormal,
				InstallDir: "/srv/files",
				DataDir:    "/srv/data",
			},
			assert: func(t *testing.T, got Instance) {
				if got.World.Name != "Core Keeper Server" {
					t.Errorf("World.Name = %q, want the default", got.World.Name)
				}
				if got.Network.Mode != NetworkSDR || got.Network.Port != 0 {
					t.Errorf("Network = %+v, want SDR without a port", got.Network)
				}
			},
		},
		{
			name:  "port switches to direct",
			input: "corekeeper",
			opts: Options{
				MaxPlayers: 5,
				Mode:       ModeHard,
				Port:       27015,
				InstallDir: "/srv/files",
				DataDir:    "/srv/data",
			},
			assert: func(t *testing.T, got Instance) {
				if got.Network.Mode != NetworkDirect || got.Network.Port != 27015 {
					t.Errorf("Network = %+v, want direct on 27015", got.Network)
				}
			},
		},
		{
			name:  "too many players",
			input: "corekeeper",
			opts: Options{
				MaxPlayers: 9,
				Mode:       ModeNormal,
				InstallDir: "/srv/files",
				DataDir:    "/srv/data",
			},
			wantErr: true,
		},
		{
			name:    "missing directories",
			input:   "corekeeper",
			opts:    Options{MaxPlayers: 4, Mode: ModeNormal},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := New(tt.input, tt.opts)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				if !errors.Is(err, ErrInvalid) {
					t.Errorf("New() error = %v, want ErrInvalid", err)
				}
				return
			}
			tt.assert(t, got)
		})
	}
}

// covers the game ID and password limits
func TestValidate(t *testing.T) {
	base := Instance{
		Name:       "corekeeper",
		Game:       GameCoreKeeper,
		InstallDir: "/srv/files",
		DataDir:    "/srv/data",
		World:      World{Mode: ModeNormal},
		Network:    Network{Mode: NetworkSDR},
		MaxPlayers: 4,
	}

	tests := []struct {
		name    string
		mutate  func(inst Instance) Instance
		wantErr bool
	}{
		{"valid", func(i Instance) Instance { return i }, false},
		{"short game id", func(i Instance) Instance { i.GameID = "abc"; return i }, true},
		{"game id with dash", func(i Instance) Instance { i.GameID = "abc-def-1234567890"; return i }, true},
		{"valid game id", func(i Instance) Instance { i.GameID = "abcdef1234567890"; return i }, false},
		{"long password", func(i Instance) Instance { i.Network.Password = strings.Repeat("x", 29); return i }, true},
		{"sdr with port", func(i Instance) Instance { i.Network.Port = 27015; return i }, true},
		{"direct without port", func(i Instance) Instance { i.Network.Mode = NetworkDirect; return i }, true},
		{"negative world index", func(i Instance) Instance { i.World.Index = -1; return i }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.mutate(base).Validate()
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
