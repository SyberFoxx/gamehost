// Package app dispatches the gamehost command line.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/syberfoxx/gamehost/internal/corekeeper"
	"github.com/syberfoxx/gamehost/internal/doctor"
	"github.com/syberfoxx/gamehost/internal/home"
	"github.com/syberfoxx/gamehost/internal/instance"
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
