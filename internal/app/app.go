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
	"time"

	"github.com/syberfoxx/gamehost/internal/home"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/process"
	"github.com/syberfoxx/gamehost/internal/store"
)

// version is overridden at build time with -ldflags "-X .../internal/app.version=...".
var version = "dev"

var (
	// ErrUsage reports an invalid command line.
	ErrUsage = errors.New("usage")
	// ErrUnhealthy reports an environment that cannot run servers yet.
	ErrUnhealthy = errors.New("environment is not ready")
	// ErrRunning reports an instance that already has a live server.
	ErrRunning = errors.New("instance is already running")
	// ErrBusy reports an instance another gamehost command is using.
	ErrBusy = errors.New("instance is in use by another gamehost command")
)

// stopGrace is how long a server gets to save before it is killed.
const stopGrace = 20 * time.Second

// logRetention is how many run logs an instance keeps.
const logRetention = 10

// defaultBackupKeep is how many archives a manual backup keeps.
const defaultBackupKeep = 5

const usage = `usage: gamehost <command> [flags] [name]

commands:
  admin     list, add or remove admins of an instance
  backup    archive the world data of an instance
  config    change or print the settings of an instance
  create    create a server instance
  import-world  copy an existing world save into an instance
  install   download the server files of an instance
  list      list instances
  logs      print or follow the newest log of an instance
  mods      list, add or remove the mods of an instance
  restore   replace the world data from a backup
  serve     serve the HTTP API and web panel
  service   install or remove the systemd units of an instance
  show      print one instance
  start     run a server, in the foreground or with --detach
  stop      stop a running server
  status    report the last known state of an instance
  update    re-download the server files of an instance
  doctor    check the environment
  version   print the gamehost version

start flags:
  --detach  run the server in the background and return

logs flags:
  -f        follow the log until Ctrl+C
  -n N      lines to print from the end (default 100)

backup flags:
  -keep N   backups to keep, oldest removed first (default 5)

admin subcommands:
  admin list <name>
  admin add <name> <id> [--name NAME] [--limited]
  admin remove <name> <id>

mods subcommands:
  mods list <name>
  mods add <name> <id> [--workshop]
  mods remove <name> <id> [--workshop]

config flags:
  --max-players N, --world-name NAME, --mode NAME, --port N, --password PW, --game-id ID
                                                                    (corekeeper)
  --port N, --udp-port N, --admin-password PW                        (projectzomboid)
  --max-players N, --port N, --cluster-token TOKEN, --offline, --caves
                                                                    (dontstarvetogether)
  --backup-every DUR, --backup-keep N    scheduled backup window, off by default

serve flags:
  --addr ADDR    listen address (default 127.0.0.1:8080)
  --start NAME   start an instance when serve boots; repeat for more

service subcommands:
  service install <name> [--enable] [--dir DIR] [--binary PATH]
  service uninstall <name> [--dir DIR]

Run gamehost with no command on a terminal to open the interactive lobby.
`

// Run executes one command and returns its error.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		// A person at a terminal gets the lobby; everything else gets the usage.
		if wantsLobby(args, stdout, os.Stdin) {
			return runLobby(ctx, stdout)
		}
		fmt.Fprint(stderr, usage)
		return ErrUsage
	}

	switch args[0] {
	case "create":
		return runCreate(args[1:], stdout, stderr)
	case "install", "update":
		return runDownload(ctx, args[0], args[1:], stdout, stderr)
	case "import-world":
		return runImportWorld(args[1:], stdout, stderr)
	case "list":
		return runList(args[1:], stdout, stderr)
	case "show":
		return runShow(args[1:], stdout, stderr)
	case "config":
		return runConfig(args[1:], stdout, stderr)
	case "mods":
		return runMods(args[1:], stdout, stderr)
	case "admin":
		return runAdmin(args[1:], stdout, stderr)
	case "start":
		return runStart(ctx, args[1:], stdout, stderr)
	case "__supervise":
		return runSupervise(ctx, args[1:], stdout, stderr)
	case "stop":
		return runStop(args[1:], stdout, stderr)
	case "logs":
		return runLogs(ctx, args[1:], stdout, stderr)
	case "backup":
		return runBackup(args[1:], stdout, stderr)
	case "restore":
		return runRestore(args[1:], stdout, stderr)
	case "serve":
		return runServe(ctx, args[1:], stdout, stderr)
	case "service":
		return runService(args[1:], stdout, stderr)
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

// writeRows prints label and value pairs as an aligned table.
func writeRows(stdout io.Writer, rows [][2]string) error {
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", row[0], row[1])
	}
	return tw.Flush()
}

// loadInstance resolves the store and loads one instance.
func loadInstance(name string) (*store.Store, instance.Instance, error) {
	s, err := openStore()
	if err != nil {
		return nil, instance.Instance{}, err
	}
	inst, err := s.Load(name)
	if err != nil {
		return nil, instance.Instance{}, err
	}
	return s, inst, nil
}

// requireStopped refuses an operation that needs exclusive access to the data
// and holds the instance lock until the returned release runs.
func requireStopped(s *store.Store, inst instance.Instance, action string) (func(), error) {
	state, err := s.LoadState(inst.Name)
	if err != nil {
		return nil, err
	}
	if state.Running() && process.Alive(state.PID) {
		return nil, fmt.Errorf("%s instance %q: %w (pid %d), stop it first", action, inst.Name, ErrRunning, state.PID)
	}

	// A server that is starting has no running state yet; the lock covers it.
	lock, err := s.Lock(inst.Name)
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			return nil, fmt.Errorf("%s instance %q: %w, wait for it", action, inst.Name, ErrBusy)
		}
		return nil, fmt.Errorf("%s instance %q: %w", action, inst.Name, err)
	}
	return func() { _ = lock.Release() }, nil
}

// humanBytes formats a byte count with a binary unit.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}

// openStore resolves the data directory and returns its store.
func openStore() (*store.Store, error) {
	root, err := home.Dir()
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	return store.New(root), nil
}

// newFlagSet returns a flag set that reports errors to stderr.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// setFlags returns the flags the user passed explicitly.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
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

// yesNo names a switch for the show output.
func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
