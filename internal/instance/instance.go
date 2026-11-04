// Package instance defines server instances and their invariants.
package instance

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// GameCoreKeeper is the only supported game for now.
const GameCoreKeeper = "corekeeper"

// Network modes, matching the dedicated server behaviour.
const (
	NetworkSDR    = "sdr"
	NetworkDirect = "direct"
)

const (
	maxNameLen     = 32
	maxPlayers     = 8
	minGameIDLen   = 15
	maxGameIDLen   = 28
	maxPasswordLen = 28
)

// ErrInvalid reports a configuration that violates an invariant.
var ErrInvalid = errors.New("invalid instance")

var (
	namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

// forbiddenGameIDChars are the characters the server rejects in a game ID.
const forbiddenGameIDChars = "Yyx0O"

// Mode is a Core Keeper world difficulty.
type Mode string

// World difficulties accepted by the dedicated server.
const (
	ModeNormal   Mode = "normal"
	ModeHard     Mode = "hard"
	ModeCreative Mode = "creative"
	ModeCasual   Mode = "casual"
)

// ParseMode validates a world mode name.
func ParseMode(value string) (Mode, error) {
	switch Mode(value) {
	case ModeNormal, ModeHard, ModeCreative, ModeCasual:
		return Mode(value), nil
	}
	return "", fmt.Errorf("unknown world mode %q (want normal, hard, creative or casual): %w", value, ErrInvalid)
}

// ServerName returns the value the dedicated server expects for this mode.
func (m Mode) ServerName() string {
	switch m {
	case ModeNormal:
		return "Normal"
	case ModeHard:
		return "Hard"
	case ModeCreative:
		return "Creative"
	case ModeCasual:
		return "Casual"
	default:
		return ""
	}
}

// World describes the world the server loads.
type World struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Seed  string `json:"seed"`
	Mode  Mode   `json:"mode"`
}

// Network describes how players reach the server.
type Network struct {
	Mode     string `json:"mode"`
	Port     int    `json:"port"`
	Password string `json:"password"`
}

// Instance is one managed server.
type Instance struct {
	Name       string  `json:"name"`
	Game       string  `json:"game"`
	InstallDir string  `json:"install_dir"`
	DataDir    string  `json:"data_dir"`
	World      World   `json:"world"`
	Network    Network `json:"network"`
	MaxPlayers int     `json:"max_players"`
	GameID     string  `json:"game_id"`
}

// Options carries the user-facing settings of a new instance.
type Options struct {
	MaxPlayers int
	WorldName  string
	Seed       string
	Mode       Mode
	Port       int
	Password   string
	GameID     string
	InstallDir string
	DataDir    string
}

// New builds an instance with defaults applied and invariants checked.
func New(name string, opts Options) (Instance, error) {
	network := Network{Mode: NetworkSDR}
	if opts.Port > 0 {
		// Setting a port is what switches the server to direct connections.
		network.Mode = NetworkDirect
		network.Port = opts.Port
	}
	network.Password = opts.Password

	world := World{
		Name: opts.WorldName,
		Seed: opts.Seed,
		Mode: opts.Mode,
	}
	if world.Name == "" {
		world.Name = "Core Keeper Server"
	}

	inst := Instance{
		Name:       name,
		Game:       GameCoreKeeper,
		InstallDir: opts.InstallDir,
		DataDir:    opts.DataDir,
		World:      world,
		Network:    network,
		MaxPlayers: opts.MaxPlayers,
		GameID:     opts.GameID,
	}
	if err := inst.Validate(); err != nil {
		return Instance{}, err
	}
	return inst, nil
}

// ValidateName rejects names that are unsafe as directory components.
func ValidateName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("instance name is empty: %w", ErrInvalid)
	case len(name) > maxNameLen:
		return fmt.Errorf("instance name %q is longer than %d characters: %w", name, maxNameLen, ErrInvalid)
	case !namePattern.MatchString(name):
		return fmt.Errorf("instance name %q must be lowercase letters, digits, - or _: %w", name, ErrInvalid)
	}
	return nil
}

// Validate checks every invariant of an instance.
func (i Instance) Validate() error {
	if err := ValidateName(i.Name); err != nil {
		return err
	}
	if i.Game != GameCoreKeeper {
		return fmt.Errorf("unsupported game %q: %w", i.Game, ErrInvalid)
	}
	if i.InstallDir == "" || i.DataDir == "" {
		return fmt.Errorf("install and data directories are required: %w", ErrInvalid)
	}
	if i.World.Index < 0 {
		return fmt.Errorf("world index %d must not be negative: %w", i.World.Index, ErrInvalid)
	}
	if _, err := ParseMode(string(i.World.Mode)); err != nil {
		return err
	}
	if i.MaxPlayers < 1 || i.MaxPlayers > maxPlayers {
		return fmt.Errorf("max players %d must be between 1 and %d: %w", i.MaxPlayers, maxPlayers, ErrInvalid)
	}
	if err := i.Network.validate(); err != nil {
		return err
	}
	if len(i.Network.Password) > maxPasswordLen {
		return fmt.Errorf("password is longer than %d characters: %w", maxPasswordLen, ErrInvalid)
	}
	// Game IDs are alphanumeric and skip characters that are easy to confuse.
	if i.GameID != "" && !validGameID(i.GameID) {
		return fmt.Errorf("game id %q must be %d-%d characters without Y, y, x, 0 or O: %w", i.GameID, minGameIDLen, maxGameIDLen, ErrInvalid)
	}
	return nil
}

// validate checks the network invariants.
func (n Network) validate() error {
	switch n.Mode {
	case NetworkSDR:
		if n.Port != 0 {
			return fmt.Errorf("port %d requires network mode %q: %w", n.Port, NetworkDirect, ErrInvalid)
		}
	case NetworkDirect:
		if n.Port < 1 || n.Port > 65535 {
			return fmt.Errorf("direct-connect port %d must be between 1 and 65535: %w", n.Port, ErrInvalid)
		}
	default:
		return fmt.Errorf("unknown network mode %q (want %q or %q): %w", n.Mode, NetworkSDR, NetworkDirect, ErrInvalid)
	}
	return nil
}

// validGameID reports whether id matches the server's accepted format.
func validGameID(id string) bool {
	if len(id) < minGameIDLen || len(id) > maxGameIDLen {
		return false
	}
	for _, r := range id {
		isAlphanumeric := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !isAlphanumeric || strings.ContainsRune(forbiddenGameIDChars, r) {
			return false
		}
	}
	return true
}
