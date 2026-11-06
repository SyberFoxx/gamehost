package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/syberfoxx/gamehost/internal/games"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/process"
	"github.com/syberfoxx/gamehost/internal/store"
	"github.com/syberfoxx/gamehost/internal/tui"
)

// lobbyItems resolves every instance for the lobby.
func lobbyItems() ([]tui.Item, error) {
	s, err := openStore()
	if err != nil {
		return nil, err
	}

	instances, err := s.List()
	if err != nil {
		return nil, err
	}

	items := make([]tui.Item, 0, len(instances))
	for _, inst := range instances {
		item, err := lobbyItem(s, inst)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// lobbyItem resolves one instance, reconciling the stored phase with the
// liveness of its process exactly like the status command does.
func lobbyItem(s *store.Store, inst instance.Instance) (tui.Item, error) {
	state, err := s.LoadState(inst.Name)
	if err != nil {
		return tui.Item{}, err
	}

	live := process.Alive(state.PID)
	state = state.Resolve(live)

	item := tui.Item{
		Name:       inst.Name,
		Game:       inst.Game,
		Phase:      string(state.Phase),
		BackupKeep: inst.Backup.Retention(),
	}
	if driver, err := games.Lookup(inst.Game); err == nil {
		if driver.Label != "" {
			item.Game = driver.Label
		}
		// The live session id only exists once the server wrote it; the state
		// keeps the last one, the same fallback status uses.
		label, value := driver.Runner.Join(inst)
		if value == "" {
			value = state.GameID
		}
		if value == "" {
			value = "(set on first start)"
		}
		item.JoinLabel, item.JoinValue = label, value
	}
	if state.Running() && live {
		item.PID = state.PID
		item.StartedAt = state.StartedAt
	}
	return item, nil
}

// lobbyShow returns the read-only detail of one instance.
func lobbyShow(name string) (tui.Detail, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return tui.Detail{}, err
	}
	return lobbyDetail(s, inst)
}

// lobbyDetail builds the detail DTO from a loaded instance.
func lobbyDetail(s *store.Store, inst instance.Instance) (tui.Detail, error) {
	item, err := lobbyItem(s, inst)
	if err != nil {
		return tui.Detail{}, err
	}
	return tui.Detail{
		Item: item,
		Rows: instanceRows(inst),
		Path: s.Layout(inst.Name).Root,
	}, nil
}

// lobbyCreate writes a new instance from the wizard values.
func lobbyCreate(settings tui.Settings) (tui.Detail, error) {
	opts, err := createOptions(settings)
	if err != nil {
		return tui.Detail{}, err
	}
	inst, err := createInstance(settings.Name, opts)
	if err != nil {
		return tui.Detail{}, err
	}
	s, err := openStore()
	if err != nil {
		return tui.Detail{}, err
	}
	return lobbyDetail(s, inst)
}

// lobbyConfig returns the editable settings of one instance.
func lobbyConfig(name string) (tui.Settings, error) {
	_, inst, err := loadInstance(name)
	if err != nil {
		return tui.Settings{}, err
	}
	return settingsOf(inst), nil
}

// lobbyUpdate saves the edited settings of one instance.
func lobbyUpdate(name string, change tui.Settings) (tui.Detail, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return tui.Detail{}, err
	}

	// The launch flags are read at start, so the server must be stopped.
	release, err := requireStopped(s, inst, "config")
	if err != nil {
		return tui.Detail{}, err
	}
	defer release()

	opts, err := updateOptions(inst, change)
	if err != nil {
		return tui.Detail{}, err
	}
	updated, err := applySettings(s, inst, opts)
	if err != nil {
		return tui.Detail{}, err
	}
	return lobbyDetail(s, updated)
}

// createOptions parses the wizard values with the rules of `create`.
func createOptions(settings tui.Settings) (instance.Options, error) {
	switch settings.Game {
	case instance.GameCoreKeeper:
		return coreKeeperOptions(settings, instance.Options{Game: instance.GameCoreKeeper})
	case instance.GameProjectZomboid:
		return projectZomboidOptions(settings, instance.Options{Game: instance.GameProjectZomboid})
	case instance.GameDontStarveTogether:
		return dontStarveTogetherOptions(settings, instance.Options{Game: instance.GameDontStarveTogether})
	default:
		return instance.Options{}, fmt.Errorf("create: unknown game %q: %w", settings.Game, ErrUsage)
	}
}

// updateOptions starts from the stored options so the directories and every
// field the form does not show survive the save.
func updateOptions(inst instance.Instance, settings tui.Settings) (instance.Options, error) {
	switch inst.Game {
	case instance.GameCoreKeeper:
		return coreKeeperOptions(settings, optionsOf(inst))
	case instance.GameProjectZomboid:
		return projectZomboidOptions(settings, optionsOf(inst))
	case instance.GameDontStarveTogether:
		return dontStarveTogetherOptions(settings, optionsOf(inst))
	default:
		return instance.Options{}, fmt.Errorf("config: unknown game %q: %w", inst.Game, ErrUsage)
	}
}

// coreKeeperOptions fills the Core Keeper half of an instance.
func coreKeeperOptions(settings tui.Settings, opts instance.Options) (instance.Options, error) {
	players, err := parseInt("max players", settings.MaxPlayers)
	if err != nil {
		return instance.Options{}, err
	}
	port, err := parseInt("port", settings.Port)
	if err != nil {
		return instance.Options{}, err
	}
	mode := strings.TrimSpace(settings.Mode)
	if mode == "" {
		mode = string(instance.ModeNormal)
	}
	worldMode, err := instance.ParseMode(mode)
	if err != nil {
		return instance.Options{}, err
	}
	every, err := parseBackupEvery(settings.BackupEvery)
	if err != nil {
		return instance.Options{}, err
	}
	keep, err := parseInt("backup keep", settings.BackupKeep)
	if err != nil {
		return instance.Options{}, err
	}

	opts.Game = instance.GameCoreKeeper
	opts.WorldName = settings.WorldName
	opts.Seed = settings.Seed
	opts.Mode = worldMode
	opts.MaxPlayers = players
	opts.Port = port
	opts.Password = settings.Password
	opts.GameID = settings.GameID
	opts.BackupEvery = every
	opts.BackupKeep = keep
	return opts, nil
}

// projectZomboidOptions fills the Project Zomboid half of an instance.
func projectZomboidOptions(settings tui.Settings, opts instance.Options) (instance.Options, error) {
	port, err := parseInt("port", settings.Port)
	if err != nil {
		return instance.Options{}, err
	}
	udp, err := parseInt("direct port", settings.UDPPort)
	if err != nil {
		return instance.Options{}, err
	}
	every, err := parseBackupEvery(settings.BackupEvery)
	if err != nil {
		return instance.Options{}, err
	}
	keep, err := parseInt("backup keep", settings.BackupKeep)
	if err != nil {
		return instance.Options{}, err
	}

	opts.Game = instance.GameProjectZomboid
	opts.WorldName = settings.WorldName
	opts.Port = port
	opts.UDPPort = udp
	opts.AdminPassword = settings.AdminPassword
	opts.BackupEvery = every
	opts.BackupKeep = keep
	return opts, nil
}

// dontStarveTogetherOptions fills the cluster half of an instance.
func dontStarveTogetherOptions(settings tui.Settings, opts instance.Options) (instance.Options, error) {
	players, err := parseInt("max players", settings.MaxPlayers)
	if err != nil {
		return instance.Options{}, err
	}
	port, err := parseInt("port", settings.Port)
	if err != nil {
		return instance.Options{}, err
	}
	every, err := parseBackupEvery(settings.BackupEvery)
	if err != nil {
		return instance.Options{}, err
	}
	keep, err := parseInt("backup keep", settings.BackupKeep)
	if err != nil {
		return instance.Options{}, err
	}

	opts.Game = instance.GameDontStarveTogether
	opts.WorldName = settings.WorldName
	opts.MaxPlayers = players
	opts.Port = port
	opts.Offline = strings.TrimSpace(settings.Network) == instance.NetworkOffline
	opts.ClusterToken = strings.TrimSpace(settings.ClusterToken)
	opts.BackupEvery = every
	opts.BackupKeep = keep
	return opts, nil
}

// parseInt accepts an empty value as zero, which is how the flags treat it.
func parseInt(field, value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a number: %w", field, value, ErrUsage)
	}
	return number, nil
}

// settingsOf turns an instance into the editable settings DTO.
func settingsOf(inst instance.Instance) tui.Settings {
	return tui.Settings{
		Game:          inst.Game,
		Name:          inst.Name,
		WorldName:     inst.World.Name,
		Seed:          inst.World.Seed,
		Mode:          string(inst.World.Mode),
		MaxPlayers:    strconv.Itoa(inst.MaxPlayers),
		Port:          strconv.Itoa(inst.Network.Port),
		UDPPort:       strconv.Itoa(inst.Network.UDPPort),
		Password:      inst.Network.Password,
		AdminPassword: inst.AdminPassword,
		GameID:        inst.GameID,
		Network:       inst.Network.Mode,
		ClusterToken:  inst.ClusterToken,
		BackupEvery:   inst.Backup.Every,
		BackupKeep:    strconv.Itoa(inst.Backup.Keep),
	}
}
