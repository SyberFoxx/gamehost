// Package app dispatches the gamehost command line.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/syberfoxx/gamehost/internal/corekeeper"
	"github.com/syberfoxx/gamehost/internal/doctor"
	"github.com/syberfoxx/gamehost/internal/home"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/process"
	"github.com/syberfoxx/gamehost/internal/steamcmd"
	"github.com/syberfoxx/gamehost/internal/store"
)

// version is overridden at build time with -ldflags "-X .../internal/app.version=...".
var version = "dev"

var (
	// ErrUsage reports an invalid command line.
	ErrUsage = errors.New("usage")
	// ErrUnhealthy reports an environment that cannot run servers yet.
	ErrUnhealthy = errors.New("environment is not ready")
)

const usage = `usage: gamehost <command> [flags] [name]

commands:
  create    create a server instance
  install   download the server files of an instance
  list      list instances
  show      print one instance
  start     run a server in the foreground until Ctrl+C
  status    report the last known state of an instance
  update    re-download the server files of an instance
  doctor    check the environment
  version   print the gamehost version
`

// Run executes one command and returns its error.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ErrUsage
	}

	switch args[0] {
	case "create":
		return runCreate(args[1:], stdout, stderr)
	case "install", "update":
		return runDownload(ctx, args[0], args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout, stderr)
	case "show":
		return runShow(args[1:], stdout, stderr)
	case "start":
		return runStart(ctx, args[1:], stdout, stderr)
	case "status":
		return runStatus(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(stdout)
	case "version":
		fmt.Fprintf(stdout, "gamehost %s\n", version)
		return nil
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return ErrUsage
	}
}

// runCreate writes a new instance configuration.
func runCreate(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("create", stderr)
	maxPlayers := fs.Int("max-players", 8, "maximum players (1-8)")
	worldName := fs.String("world-name", "", "world name shown to players")
	seed := fs.String("seed", "", "world seed, empty means random")
	mode := fs.String("mode", "normal", "world mode: normal, hard, creative or casual")
	port := fs.Int("port", 0, "direct-connect port, 0 keeps the Steam relay")
	password := fs.String("password", "", "direct-connect password, at most 28 characters")
	gameID := fs.String("game-id", "", "fixed game ID, 15-28 alphanumeric characters")
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
	worldMode, err := instance.ParseMode(*mode)
	if err != nil {
		return err
	}
	s, err := openStore()
	if err != nil {
		return err
	}

	layout := s.Layout(name)
	inst, err := instance.New(name, instance.Options{
		MaxPlayers: *maxPlayers,
		WorldName:  *worldName,
		Seed:       *seed,
		Mode:       worldMode,
		Port:       *port,
		Password:   *password,
		GameID:     *gameID,
		InstallDir: layout.Files,
		DataDir:    layout.Data,
	})
	if err != nil {
		return err
	}
	if err := s.Create(inst); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "created instance %q\n  files: %s\n  data:  %s\n", inst.Name, inst.InstallDir, inst.DataDir)
	return nil
}

// runDownload installs or updates the server files of one instance.
func runDownload(ctx context.Context, command string, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(command, stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%s needs exactly one instance name: %w", command, ErrUsage)
	}
	name := fs.Arg(0)

	s, err := openStore()
	if err != nil {
		return err
	}
	inst, err := s.Load(name)
	if err != nil {
		return err
	}
	runner, err := newRunner()
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s: downloading server files into %s\n", command, inst.InstallDir)
	if err := runner.Run(ctx, stdout, corekeeper.InstallArgs(inst.InstallDir)...); err != nil {
		return fmt.Errorf("%s instance %q: %w", command, name, err)
	}
	if _, err := os.Stat(corekeeper.BinaryPath(inst.InstallDir)); err != nil {
		return fmt.Errorf("%s instance %q: server binary missing after download: %w", command, name, err)
	}

	fmt.Fprintf(stdout, "%s: %s is ready\n", command, corekeeper.BinaryPath(inst.InstallDir))
	return nil
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
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", inst.Name, inst.Game, inst.MaxPlayers, network, inst.World.Name)
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

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	rows := [][2]string{
		{"name", inst.Name},
		{"game", inst.Game},
		{"files", inst.InstallDir},
		{"data", inst.DataDir},
		{"world", inst.World.Name},
		{"world index", fmt.Sprint(inst.World.Index)},
		{"world mode", string(inst.World.Mode)},
		{"seed", orDefault(inst.World.Seed, "(random)")},
		{"max players", fmt.Sprint(inst.MaxPlayers)},
		{"network", inst.Network.Mode},
		{"port", fmt.Sprint(inst.Network.Port)},
		{"password", orDefault(inst.Network.Password, "(none)")},
		{"game id", orDefault(inst.GameID, "(generated on first start)")},
	}
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", row[0], row[1])
	}
	return tw.Flush()
}

// runStart runs a server in the foreground until the context ends.
func runStart(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	const stopGrace = 20 * time.Second

	fs := newFlagSet("start", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("start needs exactly one instance name: %w", ErrUsage)
	}
	name := fs.Arg(0)

	s, err := openStore()
	if err != nil {
		return err
	}
	inst, err := s.Load(name)
	if err != nil {
		return err
	}
	binary := corekeeper.BinaryPath(inst.InstallDir)
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("start instance %q: server is not installed, run: gamehost install %s: %w", name, name, err)
	}

	launchArgs, err := corekeeper.Args(inst)
	if err != nil {
		return err
	}

	logPath := filepath.Join(s.Layout(name).Logs, time.Now().Format("2006-01-02_15-04-05")+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log %q: %w", logPath, err)
	}
	defer func() { _ = logFile.Close() }() // the process owns the descriptor until it exits

	server, err := process.Start(process.Spec{
		Path: binary,
		Args: launchArgs,
		Dir:  inst.InstallDir,
		Out:  io.MultiWriter(stdout, logFile),
	})
	if err != nil {
		return fmt.Errorf("start instance %q: %w", name, err)
	}

	startedAt := time.Now()
	if err := s.SaveState(name, instance.State{
		Phase:     instance.PhaseRunning,
		PID:       server.PID(),
		StartedAt: startedAt,
	}); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "started %s (pid %d), logs at %s\npress Ctrl+C to stop\n\n", name, server.PID(), logPath)

	res := server.WaitContext(ctx, stopGrace)

	phase := instance.PhaseStopped
	if !res.Stopped && res.ExitCode != 0 {
		phase = instance.PhaseCrashed
	}
	// The server writes the session id next to its binary; it is best effort
	// because a failed launch never creates the file.
	gameID, _ := corekeeper.ReadGameID(inst.InstallDir)
	if err := s.SaveState(name, instance.State{
		Phase:     phase,
		StartedAt: startedAt,
		ExitCode:  res.ExitCode,
		GameID:    gameID,
	}); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "\n%s is %s after %s (exit %d)\n", name, phase, res.Duration.Round(time.Second), res.ExitCode)
	if phase == instance.PhaseCrashed {
		return fmt.Errorf("server exited with code %d, see %s", res.ExitCode, logPath)
	}
	return nil
}

// runStatus reports the last known state of one instance.
func runStatus(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("status", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("status needs exactly one instance name: %w", ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	inst, err := s.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	state, err := s.LoadState(inst.Name)
	if err != nil {
		return err
	}

	// A running phase with a dead process means the server did not shut down cleanly.
	alive := false
	if state.Running() {
		if err := syscall.Kill(state.PID, 0); err != nil {
			state.Phase = instance.PhaseCrashed
		} else {
			alive = true
		}
	}

	rows := [][2]string{
		{"instance", inst.Name},
		{"phase", string(state.Phase)},
	}
	if alive {
		rows = append(rows,
			[2]string{"pid", fmt.Sprint(state.PID)},
			[2]string{"uptime", time.Since(state.StartedAt).Round(time.Second).String()},
		)
	}

	// The file wins: it is what the running server reports right now, while the
	// state only knows the id of the last finished run.
	gameID := state.GameID
	if id, err := corekeeper.ReadGameID(inst.InstallDir); err == nil {
		gameID = id
	}
	if gameID != "" {
		rows = append(rows, [2]string{"game id", gameID})
	}
	if state.Phase == instance.PhaseCrashed {
		rows = append(rows, [2]string{"last exit", fmt.Sprint(state.ExitCode)})
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", row[0], row[1])
	}
	return tw.Flush()
}

// runDoctor prints the environment report and fails when it is not ready.
func runDoctor(stdout io.Writer) error {
	root, err := home.Dir()
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}

	report := doctor.Run(root)
	if err := doctor.Format(stdout, report); err != nil {
		return fmt.Errorf("write doctor report: %w", err)
	}
	if !report.Healthy() {
		return ErrUnhealthy
	}
	return nil
}

// openStore resolves the data directory and returns its store.
func openStore() (*store.Store, error) {
	root, err := home.Dir()
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	return store.New(root), nil
}

// newRunner locates SteamCMD and wraps it in a runner.
func newRunner() (steamcmd.Runner, error) {
	path, err := steamcmd.Locate()
	if err != nil {
		return steamcmd.Runner{}, fmt.Errorf("%w: install SteamCMD or set %s", err, steamcmd.EnvPath)
	}
	return steamcmd.Runner{Path: path}, nil
}

// newFlagSet returns a flag set that reports errors to stderr.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// reorderFlags moves flag groups before positional arguments.
// The flag package stops parsing at the first bare argument, so without this
// "create server --max-players 5" would treat the flags as extra arguments.
func reorderFlags(fs *flag.FlagSet, args []string) []string {
	var flags, positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			return append(flags, positional...)
		case len(arg) > 1 && arg[0] == '-':
			flags = append(flags, arg)

			name := strings.TrimLeft(arg, "-")
			if strings.ContainsRune(name, '=') || fs.Lookup(name) == nil {
				continue
			}
			if boolFlag, ok := fs.Lookup(name).Value.(interface{ IsBoolFlag() bool }); ok && boolFlag.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		default:
			positional = append(positional, arg)
		}
	}
	return append(flags, positional...)
}

// usageError turns flag parsing failures into usage errors.
func usageError(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrUsage, err)
}

// orDefault returns value, or fallback when value is empty.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
