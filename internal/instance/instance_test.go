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
		{
			name:  "project zomboid defaults",
			input: "zomboid",
			opts: Options{
				Game:          GameProjectZomboid,
				AdminPassword: "secret",
				InstallDir:    "/srv/files",
				DataDir:       "/srv/data",
			},
			assert: func(t *testing.T, got Instance) {
				if got.Network.Mode != NetworkDirect || got.Network.Port != 16261 || got.Network.UDPPort != 16262 {
					t.Errorf("Network = %+v, want direct on 16261/16262", got.Network)
				}
				if got.World.Name != "servertest" || got.MaxPlayers != 0 {
					t.Errorf("instance = %+v, want the Project Zomboid defaults", got)
				}
			},
		},
		{
			name:  "project zomboid without an admin password",
			input: "zomboid",
			opts: Options{
				Game:       GameProjectZomboid,
				InstallDir: "/srv/files",
				DataDir:    "/srv/data",
			},
			wantErr: true,
		},
		{
			name:  "dont starve together defaults",
			input: "cave",
			opts: Options{
				Game:         GameDontStarveTogether,
				MaxPlayers:   6,
				ClusterToken: "pds-g^KU_test",
				InstallDir:   "/srv/files",
				DataDir:      "/srv/data",
			},
			assert: func(t *testing.T, got Instance) {
				if got.Network.Mode != NetworkOnline || got.Network.Port != DontStarveTogetherPort {
					t.Errorf("Network = %+v, want online on %d", got.Network, DontStarveTogetherPort)
				}
				if got.World.Name != "Don't Starve Together" {
					t.Errorf("World.Name = %q, want the default", got.World.Name)
				}
			},
		},
		{
			name:  "dont starve together online without a token",
			input: "cave",
			opts: Options{
				Game:       GameDontStarveTogether,
				MaxPlayers: 6,
				InstallDir: "/srv/files",
				DataDir:    "/srv/data",
			},
			wantErr: true,
		},
		{
			name:  "dont starve together offline needs no token",
			input: "lan",
			opts: Options{
				Game:       GameDontStarveTogether,
				MaxPlayers: 6,
				Offline:    true,
				InstallDir: "/srv/files",
				DataDir:    "/srv/data",
			},
			assert: func(t *testing.T, got Instance) {
				if got.Network.Mode != NetworkOffline {
					t.Errorf("Network.Mode = %q, want offline", got.Network.Mode)
				}
			},
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

// covers the scheduled backup defaults
func TestBackupSettings(t *testing.T) {
	t.Parallel()

	inst, err := New("corekeeper", Options{
		MaxPlayers:  4,
		Mode:        ModeNormal,
		BackupEvery: "24h",
		InstallDir:  "/srv/files",
		DataDir:     "/srv/data",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := inst.Backup.Interval(); got.Hours() != 24 {
		t.Errorf("Interval() = %v, want 24h", got)
	}
	if got := inst.Backup.Retention(); got != DefaultBackupKeep {
		t.Errorf("Retention() = %d, want %d", got, DefaultBackupKeep)
	}

	off, err := New("idle", Options{
		MaxPlayers: 4,
		Mode:       ModeNormal,
		InstallDir: "/srv/files",
		DataDir:    "/srv/data",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got := off.Backup.Interval(); got != 0 {
		t.Errorf("Interval() = %v, want zero for a disabled schedule", got)
	}
}

// covers the Don't Starve Together cluster rules.
func TestValidateDontStarveTogether(t *testing.T) {
	base := Instance{
		Name:         "cave",
		Game:         GameDontStarveTogether,
		InstallDir:   "/srv/files",
		DataDir:      "/srv/data",
		World:        World{Name: "Night Owls"},
		Network:      Network{Mode: NetworkOnline, Port: DontStarveTogetherPort},
		MaxPlayers:   8,
		ClusterToken: "pds-g^KU_test",
	}

	tests := []struct {
		name    string
		mutate  func(inst Instance) Instance
		wantErr bool
	}{
		{"valid online", func(i Instance) Instance { return i }, false},
		{"missing token", func(i Instance) Instance { i.ClusterToken = ""; return i }, true},
		{"token with a space", func(i Instance) Instance { i.ClusterToken = "pds g"; return i }, true},
		{"offline without a token", func(i Instance) Instance {
			i.Network.Mode = NetworkOffline
			i.ClusterToken = ""
			return i
		}, false},
		{"offline outside the LAN range", func(i Instance) Instance {
			i.Network.Mode = NetworkOffline
			i.Network.Port = 12000
			return i
		}, true},
		{"zero players", func(i Instance) Instance { i.MaxPlayers = 0; return i }, true},
		{"too many players", func(i Instance) Instance { i.MaxPlayers = 65; return i }, true},
		{"empty cluster name", func(i Instance) Instance { i.World.Name = ""; return i }, true},
		{"cluster name with a newline", func(i Instance) Instance { i.World.Name = "a\nb"; return i }, true},
		{"cluster name with an equals sign", func(i Instance) Instance { i.World.Name = "a=b"; return i }, true},
		{"cluster name too long", func(i Instance) Instance { i.World.Name = strings.Repeat("a", 65); return i }, true},
		{"core keeper seed", func(i Instance) Instance { i.World.Seed = "abc"; return i }, true},
		{"core keeper game id", func(i Instance) Instance { i.GameID = "abcdef1234567891"; return i }, true},
		{"admin password", func(i Instance) Instance { i.AdminPassword = "secret"; return i }, true},
		{"join password", func(i Instance) Instance { i.Network.Password = "secret"; return i }, true},
		{"udp port", func(i Instance) Instance { i.Network.UDPPort = 16262; return i }, true},
		{"caves online", func(i Instance) Instance { i.Caves = true; return i }, false},
		{"caves above the port range", func(i Instance) Instance {
			i.Caves = true
			i.Network.Port = 65535
			return i
		}, true},
		{"offline caves above the LAN range", func(i Instance) Instance {
			i.Network.Mode = NetworkOffline
			i.Network.Port = 11018
			i.Caves = true
			return i
		}, true},
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
		{"valid game id", func(i Instance) Instance { i.GameID = "abcdef1234567891"; return i }, false},
		{"long password", func(i Instance) Instance { i.Network.Password = strings.Repeat("x", 29); return i }, true},
		{"sdr with port", func(i Instance) Instance { i.Network.Port = 27015; return i }, true},
		{"direct without port", func(i Instance) Instance { i.Network.Mode = NetworkDirect; return i }, true},
		{"negative world index", func(i Instance) Instance { i.World.Index = -1; return i }, true},
		{"udp port", func(i Instance) Instance { i.Network.UDPPort = 16262; return i }, true},
		{"admin password", func(i Instance) Instance { i.AdminPassword = "secret"; return i }, true},
		{"caves shard", func(i Instance) Instance { i.Caves = true; return i }, true},
		{"scheduled backup", func(i Instance) Instance { i.Backup = Backup{Every: "24h", Keep: 3}; return i }, false},
		{"short backup interval", func(i Instance) Instance { i.Backup = Backup{Every: "1s", Keep: 3}; return i }, true},
		{"backup interval without a duration", func(i Instance) Instance { i.Backup = Backup{Every: "daily", Keep: 3}; return i }, true},
		{"backup keep without an interval", func(i Instance) Instance { i.Backup = Backup{Keep: 3}; return i }, true},
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

// covers the heap of the project zomboid server
func TestProjectZomboidMemory(t *testing.T) {
	t.Parallel()

	base := Options{
		Game:          GameProjectZomboid,
		AdminPassword: "secret",
		InstallDir:    "/srv/files",
		DataDir:       "/srv/data",
	}

	// An empty value stores the heap the game ships with, and a value is
	// normalised.
	for value, want := range map[string]string{
		"":      DefaultProjectZomboidMemory,
		"4g":    "4g",
		"4G":    "4g",
		"4096m": "4096m",
		"512m":  "512m",
		" 2g ":  "2g",
	} {
		opts := base
		opts.Memory = value
		inst, err := New("zomboid", opts)
		if err != nil {
			t.Fatalf("New(memory %q) error = %v", value, err)
		}
		if inst.Memory != want {
			t.Errorf("New(memory %q).Memory = %q, want %q", value, inst.Memory, want)
		}
	}

	// Anything the JVM would reject, or would boot with too little memory.
	for _, value := range []string{"4", "g", "0g", "4x", "-4g", "4 g", "256m", "4.5g", "g4"} {
		opts := base
		opts.Memory = value
		if _, err := New("zomboid", opts); !errors.Is(err, ErrInvalid) {
			t.Errorf("New(memory %q) error = %v, want ErrInvalid", value, err)
		}
	}

	// The field belongs to project zomboid alone.
	for _, game := range []string{GameCoreKeeper, GameDontStarveTogether} {
		opts := Options{Game: game, Memory: "4g", InstallDir: "/srv/files", DataDir: "/srv/data"}
		opts.MaxPlayers = 2
		if game == GameDontStarveTogether {
			opts.Offline = true
		}
		if _, err := New("server", opts); !errors.Is(err, ErrInvalid) {
			t.Errorf("New(%s, memory 4g) error = %v, want ErrInvalid", game, err)
		}
	}
}

// covers the bytes a heap setting stands for
func TestParseMemory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in       string
		wantName string
		wantByte int64
	}{
		{"8g", "8g", 8 << 30},
		{"4096m", "4096m", 4096 << 20},
		{"4096M", "4096m", 4096 << 20},
		{"8G", "8g", 8 << 30},
	}
	for _, tt := range tests {
		name, bytes, err := ParseMemory(tt.in)
		if err != nil {
			t.Fatalf("ParseMemory(%q) error = %v", tt.in, err)
		}
		if name != tt.wantName || bytes != tt.wantByte {
			t.Errorf("ParseMemory(%q) = %q, %d; want %q, %d", tt.in, name, bytes, tt.wantName, tt.wantByte)
		}
	}

	for _, value := range []string{"", "4", "0g", "4x", "256m"} {
		if _, _, err := ParseMemory(value); !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseMemory(%q) error = %v, want ErrInvalid", value, err)
		}
	}
}
