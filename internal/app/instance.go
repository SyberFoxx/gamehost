package app

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runCreate writes a new instance configuration.
func runCreate(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("create", stderr)
	gameName := fs.String("game", instance.GameCoreKeeper, "game: corekeeper or projectzomboid")
	maxPlayers := fs.Int("max-players", 8, "maximum players, 1-8 (corekeeper only)")
	worldName := fs.String("world-name", "", "world or server name shown to players")
	seed := fs.String("seed", "", "world seed, empty means random (corekeeper only)")
	mode := fs.String("mode", "normal", "world mode: normal, hard, creative or casual (corekeeper only)")
	port := fs.Int("port", 0, "game port; 0 keeps the relay (corekeeper) or uses 16261 (projectzomboid)")
	udpPort := fs.Int("udp-port", 0, "direct-connect port (projectzomboid only), default game port + 1")
	password := fs.String("password", "", "join password, at most 28 characters (corekeeper only)")
	adminPassword := fs.String("admin-password", "", "admin password (projectzomboid only)")
	gameID := fs.String("game-id", "", "fixed game ID, 15-28 alphanumeric characters (corekeeper only)")
	backupEvery := fs.String("backup-every", "", "scheduled backup interval such as 24h; off by default")
	backupKeep := fs.Int("backup-keep", 0, "scheduled archives to keep (default 5)")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("create needs exactly one instance name: %w", ErrUsage)
	}

	name := fs.Arg(0)
	if err := instance.ValidateName(name); err != nil {
		return err
	}

	opts := instance.Options{
		Game:      *gameName,
		WorldName: *worldName,
	}
	set := setFlags(fs)
	if err := rejectForeignFlags("create", *gameName, set); err != nil {
		return err
	}
	every, err := parseBackupEvery(*backupEvery)
	if err != nil {
		return err
	}
	opts.BackupEvery = every
	opts.BackupKeep = *backupKeep

	switch *gameName {
	case instance.GameCoreKeeper:
		worldMode, err := instance.ParseMode(*mode)
		if err != nil {
			return err
		}
		opts.MaxPlayers = *maxPlayers
		opts.Mode = worldMode
		opts.Seed = *seed
		opts.Port = *port
		opts.Password = *password
		opts.GameID = *gameID
	case instance.GameProjectZomboid:
		opts.Port = *port
		opts.UDPPort = *udpPort
		opts.AdminPassword = *adminPassword
	default:
		return fmt.Errorf("create: unknown game %q: %w", *gameName, ErrUsage)
	}

	inst, err := createInstance(name, opts)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "created instance %q\n  files: %s\n  data:  %s\n", inst.Name, inst.InstallDir, inst.DataDir)
	return nil
}

// createInstance resolves the layout and writes a new instance.
func createInstance(name string, opts instance.Options) (instance.Instance, error) {
	if err := instance.ValidateName(name); err != nil {
		return instance.Instance{}, err
	}

	s, err := openStore()
	if err != nil {
		return instance.Instance{}, err
	}
	layout := s.Layout(name)
	opts.InstallDir = layout.Files
	opts.DataDir = layout.Data

	inst, err := instance.New(name, opts)
	if err != nil {
		return instance.Instance{}, err
	}
	if err := s.Create(inst); err != nil {
		return instance.Instance{}, err
	}
	return inst, nil
}

// applySettings rebuilds an instance from changed options and stores it.
func applySettings(s *store.Store, inst instance.Instance, opts instance.Options) (instance.Instance, error) {
	updated, err := instance.New(inst.Name, opts)
	if err != nil {
		return instance.Instance{}, err
	}
	if err := s.Save(updated); err != nil {
		return instance.Instance{}, err
	}
	return updated, nil
}

// rejectForeignFlags refuses flags that belong to the other game.
func rejectForeignFlags(verb, game string, set map[string]bool) error {
	foreign := []string{"udp-port", "admin-password"}
	owner := instance.GameProjectZomboid
	if game == instance.GameProjectZomboid {
		foreign = []string{"max-players", "seed", "mode", "password", "game-id"}
		owner = instance.GameCoreKeeper
	}
	for _, name := range foreign {
		if set[name] {
			return fmt.Errorf("%s: --%s is only used by %s: %w", verb, name, owner, ErrUsage)
		}
	}
	return nil
}

// parseBackupEvery accepts a duration or an explicit switch-off value.
func parseBackupEvery(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "off", "none", "0":
		return "", nil
	}
	return value, nil
}

// optionsOf turns an instance back into the settings a new one is built from.
func optionsOf(inst instance.Instance) instance.Options {
	return instance.Options{
		Game:          inst.Game,
		MaxPlayers:    inst.MaxPlayers,
		WorldName:     inst.World.Name,
		Seed:          inst.World.Seed,
		Mode:          inst.World.Mode,
		Port:          inst.Network.Port,
		UDPPort:       inst.Network.UDPPort,
		Password:      inst.Network.Password,
		AdminPassword: inst.AdminPassword,
		GameID:        inst.GameID,
		BackupEvery:   inst.Backup.Every,
		BackupKeep:    inst.Backup.Keep,
		InstallDir:    inst.InstallDir,
		DataDir:       inst.DataDir,
	}
}

// runList prints every instance as a table.
func runList(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("list", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("list takes no arguments: %w", ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	instances, err := s.List()
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		fmt.Fprintln(stdout, "no instances yet, create one with: gamehost create <name>")
		return nil
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tGAME\tPLAYERS\tNETWORK\tWORLD")
	for _, inst := range instances {
		network := inst.Network.Mode
		if inst.Network.Port > 0 {
			network = fmt.Sprintf("%s:%d", network, inst.Network.Port)
		}
		if inst.Network.UDPPort > 0 {
			network = fmt.Sprintf("%s:%d/%d", inst.Network.Mode, inst.Network.Port, inst.Network.UDPPort)
		}
		players := fmt.Sprint(inst.MaxPlayers)
		if inst.MaxPlayers == 0 {
			players = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", inst.Name, inst.Game, players, network, inst.World.Name)
	}
	return tw.Flush()
}

// runShow prints one instance field by field.
func runShow(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("show", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("show needs exactly one instance name: %w", ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	inst, err := s.Load(fs.Arg(0))
	if err != nil {
		return err
	}

	return writeRows(stdout, instanceRows(inst))
}

// runConfig changes the settings of an existing instance without editing JSON.
func runConfig(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("config", stderr)
	maxPlayers := fs.Int("max-players", 0, "maximum players, 1-8 (corekeeper only)")
	worldName := fs.String("world-name", "", "world name shown to players (corekeeper only)")
	mode := fs.String("mode", "", "world mode: normal, hard, creative or casual (corekeeper only)")
	port := fs.Int("port", 0, "game port; 0 keeps the relay (corekeeper)")
	udpPort := fs.Int("udp-port", 0, "direct-connect port (projectzomboid only)")
	password := fs.String("password", "", "join password, empty clears it (corekeeper only)")
	adminPassword := fs.String("admin-password", "", "admin password (projectzomboid only)")
	gameID := fs.String("game-id", "", "fixed game ID, empty generates one on the next start (corekeeper only)")
	backupEvery := fs.String("backup-every", "", "scheduled backup interval such as 24h; off disables it")
	backupKeep := fs.Int("backup-keep", 0, "scheduled archives to keep")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("config needs exactly one instance name: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}

	set := setFlags(fs)
	if len(set) == 0 {
		return writeRows(stdout, instanceRows(inst))
	}

	// The server reads these settings at launch, so it must not be running.
	release, err := requireStopped(s, inst, "config")
	if err != nil {
		return err
	}
	defer release()

	if err := rejectForeignFlags("config", inst.Game, set); err != nil {
		return err
	}
	opts := optionsOf(inst)
	switch inst.Game {
	case instance.GameCoreKeeper:
		if set["max-players"] {
			opts.MaxPlayers = *maxPlayers
		}
		if set["world-name"] {
			opts.WorldName = *worldName
		}
		if set["mode"] {
			worldMode, err := instance.ParseMode(*mode)
			if err != nil {
				return err
			}
			opts.Mode = worldMode
		}
		if set["port"] {
			opts.Port = *port
		}
		if set["password"] {
			opts.Password = *password
		}
		if set["game-id"] {
			opts.GameID = *gameID
		}
	case instance.GameProjectZomboid:
		if set["port"] {
			opts.Port = *port
		}
		if set["udp-port"] {
			opts.UDPPort = *udpPort
		}
		if set["admin-password"] {
			opts.AdminPassword = *adminPassword
		}
	}
	if set["backup-every"] {
		every, err := parseBackupEvery(*backupEvery)
		if err != nil {
			return err
		}
		opts.BackupEvery = every
		if every == "" {
			opts.BackupKeep = 0
		}
	}
	if set["backup-keep"] {
		opts.BackupKeep = *backupKeep
	}

	updated, err := applySettings(s, inst, opts)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "updated %s\n", updated.Name)
	return writeRows(stdout, instanceRows(updated))
}

// instanceRows returns the configuration of one instance as label and value pairs.
func instanceRows(inst instance.Instance) [][2]string {
	rows := [][2]string{
		{"name", inst.Name},
		{"game", inst.Game},
		{"files", inst.InstallDir},
		{"data", inst.DataDir},
		{"world", inst.World.Name},
		{"network", inst.Network.Mode},
		{"port", fmt.Sprint(inst.Network.Port)},
	}
	if inst.Game == instance.GameProjectZomboid {
		return append(rows,
			[2]string{"direct port", fmt.Sprint(inst.Network.UDPPort)},
			[2]string{"admin password", orDefault(inst.AdminPassword, "(none)")},
			[2]string{"max players", "from the server ini"},
			[2]string{"scheduled backup", backupSummary(inst.Backup)},
		)
	}
	return append(rows,
		[2]string{"world index", fmt.Sprint(inst.World.Index)},
		[2]string{"world mode", string(inst.World.Mode)},
		[2]string{"seed", orDefault(inst.World.Seed, "(random)")},
		[2]string{"max players", fmt.Sprint(inst.MaxPlayers)},
		[2]string{"password", orDefault(inst.Network.Password, "(none)")},
		[2]string{"game id", orDefault(inst.GameID, "(generated on first start)")},
		[2]string{"scheduled backup", backupSummary(inst.Backup)},
	)
}

// backupSummary describes the schedule in one cell.
func backupSummary(b instance.Backup) string {
	if b.Interval() == 0 {
		return "(off)"
	}
	return fmt.Sprintf("every %s, keep %d", b.Every, b.Retention())
}
