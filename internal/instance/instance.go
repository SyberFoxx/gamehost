// Package instance defines server instances and their invariants.
package instance

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Supported games.
const (
	GameCoreKeeper         = "corekeeper"
	GameProjectZomboid     = "projectzomboid"
	GameDontStarveTogether = "dontstarvetogether"
)

// Network modes, matching the dedicated server behaviour.
const (
	NetworkSDR     = "sdr"
	NetworkDirect  = "direct"
	NetworkOnline  = "online"
	NetworkOffline = "offline"
)

const (
	maxNameLen         = 32
	maxPlayers         = 8
	minGameIDLen       = 15
	maxGameIDLen       = 28
	maxPasswordLen     = 28
	projectZomboidPort = 16261

	// DontStarveTogetherMaxPlayers is the largest lobby the server accepts.
	DontStarveTogetherMaxPlayers = 64
	// DontStarveTogetherPort is the default UDP port of a cluster.
	DontStarveTogetherPort = 10999
	// An offline cluster is LAN only and refuses to leave this range.
	dstLanMinPort      = 10998
	dstLanMaxPort      = 11018
	maxClusterNameLen  = 64
	maxClusterTokenLen = 512
)

// DefaultBackupKeep is how many scheduled archives an instance keeps.
const DefaultBackupKeep = 5

// minBackupEvery keeps a schedule from restarting a server in a tight loop.
const minBackupEvery = 10 * time.Second

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
	UDPPort  int    `json:"udp_port,omitempty"`
	Password string `json:"password"`
}

// Backup describes the scheduled snapshots of an instance.
type Backup struct {
	// Every is a Go duration such as "24h"; empty disables the schedule.
	Every string `json:"every,omitempty"`
	Keep  int    `json:"keep,omitempty"`
}

// Interval returns the backup period, or zero when the schedule is off.
func (b Backup) Interval() time.Duration {
	period, err := time.ParseDuration(b.Every)
	if err != nil {
		return 0
	}
	return period
}

// Retention returns how many archives the schedule keeps.
func (b Backup) Retention() int {
	if b.Keep > 0 {
		return b.Keep
	}
	return DefaultBackupKeep
}

// Instance is one managed server.
type Instance struct {
	Name          string  `json:"name"`
	Game          string  `json:"game"`
	InstallDir    string  `json:"install_dir"`
	DataDir       string  `json:"data_dir"`
	World         World   `json:"world"`
	Network       Network `json:"network"`
	MaxPlayers    int     `json:"max_players"`
	GameID        string  `json:"game_id"`
	AdminPassword string  `json:"admin_password,omitempty"`
	// ClusterToken authorises an online Don't Starve Together cluster; the
	// driver writes it to cluster_token.txt before the launch.
	ClusterToken string `json:"cluster_token,omitempty"`
	Backup       Backup `json:"backup"`
}

// Options carries the user-facing settings of a new instance.
type Options struct {
	Game          string
	MaxPlayers    int
	WorldName     string
	Seed          string
	Mode          Mode
	Port          int
	UDPPort       int
	Password      string
	AdminPassword string
	GameID        string
	ClusterToken  string
	Offline       bool
	BackupEvery   string
	BackupKeep    int
	InstallDir    string
	DataDir       string
}

// New builds an instance with defaults applied and invariants checked.
func New(name string, opts Options) (Instance, error) {
	game := opts.Game
	if game == "" {
		game = GameCoreKeeper
	}

	network := Network{Mode: NetworkSDR}
	switch game {
	case GameCoreKeeper:
		if opts.Port > 0 {
			// Setting a port is what switches the server to direct connections.
			network.Mode = NetworkDirect
			network.Port = opts.Port
		}
		network.Password = opts.Password
	case GameProjectZomboid:
		// Project Zomboid always listens on a game port and a direct port.
		network.Mode = NetworkDirect
		network.Port = opts.Port
		if network.Port == 0 {
			network.Port = projectZomboidPort
		}
		network.UDPPort = opts.UDPPort
		if network.UDPPort == 0 && network.Port < 65535 {
			network.UDPPort = network.Port + 1
		}
	case GameDontStarveTogether:
		// A DST cluster is either listed by Klei or LAN only; both listen on UDP.
		network.Mode = NetworkOnline
		if opts.Offline {
			network.Mode = NetworkOffline
		}
		network.Port = opts.Port
		if network.Port == 0 {
			network.Port = DontStarveTogetherPort
		}
	}

	world := World{
		Name: opts.WorldName,
		Seed: opts.Seed,
		Mode: opts.Mode,
	}
	if world.Name == "" {
		world.Name = "Core Keeper Server"
		switch game {
		case GameProjectZomboid:
			world.Name = "servertest"
		case GameDontStarveTogether:
			world.Name = "Don't Starve Together"
		}
	}

	backup := Backup{Every: opts.BackupEvery, Keep: opts.BackupKeep}
	if backup.Every != "" && backup.Keep == 0 {
		backup.Keep = DefaultBackupKeep
	}

	inst := Instance{
		Name:          name,
		Game:          game,
		InstallDir:    opts.InstallDir,
		DataDir:       opts.DataDir,
		World:         world,
		Network:       network,
		MaxPlayers:    opts.MaxPlayers,
		GameID:        opts.GameID,
		AdminPassword: opts.AdminPassword,
		ClusterToken:  opts.ClusterToken,
		Backup:        backup,
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
	if i.InstallDir == "" || i.DataDir == "" {
		return fmt.Errorf("install and data directories are required: %w", ErrInvalid)
	}
	if err := i.Backup.validate(); err != nil {
		return err
	}
	switch i.Game {
	case GameCoreKeeper:
		return i.validateCoreKeeper()
	case GameProjectZomboid:
		return i.validateProjectZomboid()
	case GameDontStarveTogether:
		return i.validateDontStarveTogether()
	default:
		return fmt.Errorf("unsupported game %q: %w", i.Game, ErrInvalid)
	}
}

// validateCoreKeeper checks the launch contract of the Core Keeper server.
func (i Instance) validateCoreKeeper() error {
	if i.World.Index < 0 {
		return fmt.Errorf("world index %d must not be negative: %w", i.World.Index, ErrInvalid)
	}
	if _, err := ParseMode(string(i.World.Mode)); err != nil {
		return err
	}
	if i.MaxPlayers < 1 || i.MaxPlayers > maxPlayers {
		return fmt.Errorf("max players %d must be between 1 and %d: %w", i.MaxPlayers, maxPlayers, ErrInvalid)
	}
	if i.Network.UDPPort != 0 {
		return fmt.Errorf("udp port %d is only used by project zomboid: %w", i.Network.UDPPort, ErrInvalid)
	}
	if err := i.Network.validate(); err != nil {
		return err
	}
	if len(i.Network.Password) > maxPasswordLen {
		return fmt.Errorf("password is longer than %d characters: %w", maxPasswordLen, ErrInvalid)
	}
	if i.AdminPassword != "" {
		return fmt.Errorf("admin password is only used by project zomboid: %w", ErrInvalid)
	}
	// Game IDs are alphanumeric and skip characters that are easy to confuse.
	if i.GameID != "" && !validGameID(i.GameID) {
		return fmt.Errorf("game id %q must be %d-%d characters without Y, y, x, 0 or O: %w", i.GameID, minGameIDLen, maxGameIDLen, ErrInvalid)
	}
	return nil
}

// validateProjectZomboid checks the launch contract of the Project Zomboid server.
func (i Instance) validateProjectZomboid() error {
	if i.World.Index != 0 || i.World.Seed != "" || i.World.Mode != "" {
		return fmt.Errorf("world index, seed and mode belong to core keeper: %w", ErrInvalid)
	}
	// The server name is also the save folder and ini name, so keep it path safe.
	if len(i.World.Name) > maxNameLen || !namePattern.MatchString(i.World.Name) {
		return fmt.Errorf("server name %q must be lowercase letters, digits, - or _: %w", i.World.Name, ErrInvalid)
	}
	// Player limits and the join password live in the server ini, not in flags.
	if i.MaxPlayers != 0 {
		return fmt.Errorf("max players are configured in the Project Zomboid ini, not by gamehost: %w", ErrInvalid)
	}
	if i.GameID != "" {
		return fmt.Errorf("game id belongs to core keeper: %w", ErrInvalid)
	}
	if i.Network.Password != "" {
		return fmt.Errorf("join password is configured in the Project Zomboid ini, not by gamehost: %w", ErrInvalid)
	}
	// The first start waits for an admin password unless it is passed up front.
	if i.AdminPassword == "" {
		return fmt.Errorf("admin password is required for project zomboid: %w", ErrInvalid)
	}
	if i.Network.Mode != NetworkDirect {
		return fmt.Errorf("project zomboid always uses direct connections: %w", ErrInvalid)
	}
	if i.Network.Port < 1 || i.Network.Port > 65535 {
		return fmt.Errorf("game port %d must be between 1 and 65535: %w", i.Network.Port, ErrInvalid)
	}
	if i.Network.UDPPort < 1 || i.Network.UDPPort > 65535 {
		return fmt.Errorf("direct port %d must be between 1 and 65535: %w", i.Network.UDPPort, ErrInvalid)
	}
	if i.Network.UDPPort == i.Network.Port {
		return fmt.Errorf("game and direct ports must differ (both %d): %w", i.Network.Port, ErrInvalid)
	}
	return nil
}

// validateDontStarveTogether checks the launch contract of a DST cluster.
func (i Instance) validateDontStarveTogether() error {
	if i.World.Index != 0 || i.World.Seed != "" || i.World.Mode != "" {
		return fmt.Errorf("world index, seed and mode belong to core keeper: %w", ErrInvalid)
	}
	if err := validateClusterName(i.World.Name); err != nil {
		return err
	}
	if i.MaxPlayers < 1 || i.MaxPlayers > DontStarveTogetherMaxPlayers {
		return fmt.Errorf("max players %d must be between 1 and %d: %w", i.MaxPlayers, DontStarveTogetherMaxPlayers, ErrInvalid)
	}
	if i.GameID != "" {
		return fmt.Errorf("game id belongs to core keeper: %w", ErrInvalid)
	}
	if i.AdminPassword != "" {
		return fmt.Errorf("admin password is only used by project zomboid: %w", ErrInvalid)
	}
	// The join password lives in cluster.ini, which the server owns.
	if i.Network.Password != "" {
		return fmt.Errorf("the cluster password is configured in cluster.ini, not by gamehost: %w", ErrInvalid)
	}
	if i.Network.UDPPort != 0 {
		return fmt.Errorf("udp port %d is only used by project zomboid: %w", i.Network.UDPPort, ErrInvalid)
	}

	switch i.Network.Mode {
	case NetworkOnline:
		if err := validateClusterToken(i.ClusterToken); err != nil {
			return err
		}
	case NetworkOffline:
		// The server refuses an offline cluster outside its LAN port range.
		if i.Network.Port < dstLanMinPort || i.Network.Port > dstLanMaxPort {
			return fmt.Errorf("offline cluster port %d must be between %d and %d: %w", i.Network.Port, dstLanMinPort, dstLanMaxPort, ErrInvalid)
		}
	default:
		return fmt.Errorf("unknown network mode %q (want %q or %q): %w", i.Network.Mode, NetworkOnline, NetworkOffline, ErrInvalid)
	}
	if i.Network.Port < 1 || i.Network.Port > 65535 {
		return fmt.Errorf("game port %d must be between 1 and 65535: %w", i.Network.Port, ErrInvalid)
	}
	return nil
}

// validateClusterName keeps the name safe to write into cluster.ini.
func validateClusterName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("cluster name is empty: %w", ErrInvalid)
	case len([]rune(name)) > maxClusterNameLen:
		return fmt.Errorf("cluster name is longer than %d characters: %w", maxClusterNameLen, ErrInvalid)
	}
	for _, r := range name {
		if r < ' ' || r == 0x7f || strings.ContainsRune(`=[]`+"\n\r", r) {
			return fmt.Errorf("cluster name %q must not contain control characters, =, [ or ]: %w", name, ErrInvalid)
		}
	}
	return nil
}

// validateClusterToken rejects a token that cannot come from Klei's site.
func validateClusterToken(token string) error {
	if token == "" {
		return fmt.Errorf("an online cluster needs a Klei cluster token: %w", ErrInvalid)
	}
	if token != strings.TrimSpace(token) {
		return fmt.Errorf("cluster token must not start or end with whitespace: %w", ErrInvalid)
	}
	if len(token) > maxClusterTokenLen {
		return fmt.Errorf("cluster token is longer than %d characters: %w", maxClusterTokenLen, ErrInvalid)
	}
	for _, r := range token {
		if r <= ' ' || r == 0x7f {
			return fmt.Errorf("cluster token must not contain spaces or control characters: %w", ErrInvalid)
		}
	}
	return nil
}

// validate checks the scheduled snapshot settings.
func (b Backup) validate() error {
	if b.Every == "" {
		if b.Keep != 0 {
			return fmt.Errorf("backup keep %d needs a backup interval: %w", b.Keep, ErrInvalid)
		}
		return nil
	}

	period, err := time.ParseDuration(b.Every)
	if err != nil {
		return fmt.Errorf("backup interval %q is not a duration such as 24h: %w", b.Every, ErrInvalid)
	}
	if period < minBackupEvery {
		return fmt.Errorf("backup interval %s is shorter than %s: %w", period, minBackupEvery, ErrInvalid)
	}
	if b.Keep < 1 {
		return fmt.Errorf("backup keep %d must be at least 1: %w", b.Keep, ErrInvalid)
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
