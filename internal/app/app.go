// Package app dispatches the gamehost command line.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/syberfoxx/gamehost/internal/backup"
	"github.com/syberfoxx/gamehost/internal/doctor"
	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/games"
	"github.com/syberfoxx/gamehost/internal/home"
	"github.com/syberfoxx/gamehost/internal/httpapi"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/logs"
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
	// ErrRunning reports an instance that already has a live server.
	ErrRunning = errors.New("instance is already running")
)

// stopGrace is how long a server gets to save before it is killed.
const stopGrace = 20 * time.Second

const usage = `usage: gamehost <command> [flags] [name]

commands:
  backup    archive the world data of an instance
  create    create a server instance
  install   download the server files of an instance
  list      list instances
  logs      print or follow the newest log of an instance
  restore   replace the world data from a backup
  serve     serve the HTTP API and web panel
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

serve flags:
  --addr ADDR  listen address (default 127.0.0.1:8080)
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
	s, err := openStore()
	if err != nil {
		return err
	}

	layout := s.Layout(name)
	opts := instance.Options{
		Game:       *gameName,
		WorldName:  *worldName,
		InstallDir: layout.Files,
		DataDir:    layout.Data,
	}
	set := setFlags(fs)
	switch *gameName {
	case instance.GameCoreKeeper:
		for _, name := range []string{"udp-port", "admin-password"} {
			if set[name] {
				return fmt.Errorf("create: --%s is only used by projectzomboid: %w", name, ErrUsage)
			}
		}
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
		for _, name := range []string{"max-players", "seed", "mode", "password", "game-id"} {
			if set[name] {
				return fmt.Errorf("create: --%s is only used by corekeeper: %w", name, ErrUsage)
			}
		}
		opts.Port = *port
		opts.UDPPort = *udpPort
		opts.AdminPassword = *adminPassword
	default:
		return fmt.Errorf("create: unknown game %q: %w", *gameName, ErrUsage)
	}

	inst, err := instance.New(name, opts)
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

	// An update rewrites the server files under a running process, and the
	// archive is only consistent while the world is closed.
	if command == "update" {
		if err := requireStopped(s, inst, command); err != nil {
			return err
		}
		if _, err := backupBeforeUpdate(s, inst, stdout); err != nil {
			return err
		}
	}

	runner, err := newRunner()
	if err != nil {
		return err
	}

	driver, err := games.Lookup(inst.Game)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s: downloading server files into %s\n", command, inst.InstallDir)
	if err := runner.Run(ctx, stdout, driver.Installer.InstallArgs(inst.InstallDir)...); err != nil {
		return fmt.Errorf("%s instance %q: %w", command, name, err)
	}
	serverPath := driver.Installer.ServerPath(inst.InstallDir)
	if _, err := os.Stat(serverPath); err != nil {
		return fmt.Errorf("%s instance %q: server binary missing after download: %w", command, name, err)
	}

	fmt.Fprintf(stdout, "%s: %s is ready\n", command, serverPath)
	return nil
}

// backupBeforeUpdate archives the data directory so an update can be undone.
// It returns an empty path when the instance has no data yet.
func backupBeforeUpdate(s *store.Store, inst instance.Instance, stdout io.Writer) (string, error) {
	layout := s.Layout(inst.Name)
	if _, err := os.Stat(layout.Data); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("inspect data directory of %q: %w", inst.Name, err)
	}
	if err := os.MkdirAll(layout.Backups, 0o755); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}

	path := filepath.Join(layout.Backups, "pre-update_"+time.Now().Format("2006-01-02_15-04-05")+".tar.gz")
	res, err := backup.Create(path, layout.Data)
	if err != nil {
		return "", fmt.Errorf("backup %q before the update: %w", inst.Name, err)
	}
	fmt.Fprintf(stdout, "backed up %s before the update: %s (%d files, %s)\n",
		inst.Name, res.Path, res.Files, humanBytes(res.Bytes))
	return res.Path, nil
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

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
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
		rows = append(rows,
			[2]string{"direct port", fmt.Sprint(inst.Network.UDPPort)},
			[2]string{"admin password", orDefault(inst.AdminPassword, "(none)")},
			[2]string{"max players", "from the server ini"},
		)
	} else {
		rows = append(rows,
			[2]string{"world index", fmt.Sprint(inst.World.Index)},
			[2]string{"world mode", string(inst.World.Mode)},
			[2]string{"seed", orDefault(inst.World.Seed, "(random)")},
			[2]string{"max players", fmt.Sprint(inst.MaxPlayers)},
			[2]string{"password", orDefault(inst.Network.Password, "(none)")},
			[2]string{"game id", orDefault(inst.GameID, "(generated on first start)")},
		)
	}
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", row[0], row[1])
	}
	return tw.Flush()
}

// runStart runs a server in the foreground, or hands it to a detached supervisor.
func runStart(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("start", stderr)
	detach := fs.Bool("detach", false, "run the server in the background and return")
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

	launch, logPath, err := prepareStart(s, inst)
	if err != nil {
		return err
	}

	if *detach {
		return startDetached(s, inst, logPath, stdout)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log %q: %w", logPath, err)
	}
	defer func() { _ = logFile.Close() }() // the process owns the descriptor until it exits

	return serve(ctx, s, inst, launch, io.MultiWriter(stdout, logFile), logPath, true)
}

// prepareStart validates a launch and returns its command and log path.
// Two servers on one data directory corrupt the world, so it refuses early.
func prepareStart(s *store.Store, inst instance.Instance) (game.Launch, string, error) {
	driver, err := games.Lookup(inst.Game)
	if err != nil {
		return game.Launch{}, "", err
	}
	state, err := s.LoadState(inst.Name)
	if err != nil {
		return game.Launch{}, "", err
	}
	if state.Running() && process.Alive(state.PID) {
		return game.Launch{}, "", fmt.Errorf("start instance %q: %w (pid %d)", inst.Name, ErrRunning, state.PID)
	}
	if _, err := os.Stat(driver.Installer.ServerPath(inst.InstallDir)); err != nil {
		return game.Launch{}, "", fmt.Errorf("start instance %q: server is not installed, run: gamehost install %s: %w", inst.Name, inst.Name, err)
	}

	// Validate the launch contract before a process exists.
	launch, err := driver.Runner.Launch(inst)
	if err != nil {
		return game.Launch{}, "", err
	}
	logPath := filepath.Join(s.Layout(inst.Name).Logs, time.Now().Format("2006-01-02_15-04-05")+".log")
	return launch, logPath, nil
}

// startDetached re-executes gamehost as a supervisor in a new session.
func startDetached(s *store.Store, inst instance.Instance, logPath string, stdout io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve the gamehost binary: %w", err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log %q: %w", logPath, err)
	}
	defer func() { _ = logFile.Close() }() // the supervisor inherits its own descriptor

	// Claim the instance before the child starts, so a concurrent start sees it.
	if err := s.SaveState(inst.Name, instance.State{
		Phase:     instance.PhaseStarting,
		StartedAt: time.Now(),
	}); err != nil {
		return err
	}

	supervisorPID, err := process.StartDetached(process.Spec{
		Path: exe,
		Args: []string{"__supervise", inst.Name, "--log", logPath},
		Out:  logFile,
	})
	if err != nil {
		_ = s.SaveState(inst.Name, instance.State{Phase: instance.PhaseStopped})
		return fmt.Errorf("start instance %q: %w", inst.Name, err)
	}

	// The supervisor publishes the server pid once it exists.
	const startupWait = 15 * time.Second
	deadline := time.Now().Add(startupWait)
	for time.Now().Before(deadline) {
		state, err := s.LoadState(inst.Name)
		if err != nil {
			return err
		}
		switch {
		case state.Phase == instance.PhaseRunning && state.PID > 0:
			fmt.Fprintf(stdout, "%s is running in the background (pid %d)\n  logs: %s\n  stop: gamehost stop %s\n",
				inst.Name, state.PID, logPath, inst.Name)
			return nil
		case state.Phase == instance.PhaseCrashed:
			return fmt.Errorf("start instance %q: the server exited during startup, see %s", inst.Name, logPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("start instance %q: supervisor %d did not report a server within %s, see %s",
		inst.Name, supervisorPID, startupWait, logPath)
}

// serve starts the server and reports its lifecycle until it exits.
// It runs in the foreground or inside the detached supervisor.
func serve(ctx context.Context, s *store.Store, inst instance.Instance, launch game.Launch, out io.Writer, logPath string, foreground bool) error {
	server, err := process.Start(process.Spec{
		Path: launch.Path,
		Args: launch.Args,
		Dir:  launch.Dir,
		Out:  out,
	})
	if err != nil {
		return fmt.Errorf("start instance %q: %w", inst.Name, err)
	}

	startedAt := time.Now()
	if err := s.SaveState(inst.Name, instance.State{
		Phase:     instance.PhaseRunning,
		PID:       server.PID(),
		StartedAt: startedAt,
	}); err != nil {
		return err
	}

	if foreground {
		fmt.Fprintf(out, "started %s (pid %d), logs at %s\npress Ctrl+C to stop\n\n", inst.Name, server.PID(), logPath)
	} else {
		fmt.Fprintf(out, "supervising %s (pid %d)\n", inst.Name, server.PID())
	}

	res := server.WaitContext(ctx, stopGrace)

	// A stop requested from another terminal is a clean stop even when the
	// server aborts on the way out; only an unrequested failure is a crash.
	if state, err := s.LoadState(inst.Name); err == nil && state.Phase == instance.PhaseStopping {
		res.Stopped = true
	}

	phase := instance.PhaseStopped
	if !res.Stopped && res.ExitCode != 0 {
		phase = instance.PhaseCrashed
	}
	// The session id is best effort: a failed launch never writes one, and
	// games without join codes leave it empty.
	sessionID := ""
	if driver, err := games.Lookup(inst.Game); err == nil {
		sessionID = driver.Runner.SessionID(inst)
	}
	if err := s.SaveState(inst.Name, instance.State{
		Phase:     phase,
		StartedAt: startedAt,
		ExitCode:  res.ExitCode,
		GameID:    sessionID,
	}); err != nil {
		return err
	}

	fmt.Fprintf(out, "\n%s is %s after %s (exit %d)\n", inst.Name, phase, res.Duration.Round(time.Second), res.ExitCode)
	if phase == instance.PhaseCrashed {
		return fmt.Errorf("server exited with code %d, see %s", res.ExitCode, logPath)
	}
	return nil
}

// runSupervise is the hidden half of start --detach: one instance, one session.
func runSupervise(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("__supervise", stderr)
	logPath := fs.String("log", "", "log file used for messages")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("__supervise needs exactly one instance name: %w", ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	inst, err := s.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	driver, err := games.Lookup(inst.Game)
	if err != nil {
		return err
	}
	launch, err := driver.Runner.Launch(inst)
	if err != nil {
		return err
	}
	return serve(ctx, s, inst, launch, stdout, *logPath, false)
}

// runStop asks a running server to exit and waits for it.
func runStop(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("stop", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("stop needs exactly one instance name: %w", ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	inst, err := s.Load(fs.Arg(0))
	if err != nil {
		return err
	}
	return stopInstance(s, inst, stdout)
}

// stopInstance asks a running server to exit and waits for it.
func stopInstance(s *store.Store, inst instance.Instance, stdout io.Writer) error {
	state, err := s.LoadState(inst.Name)
	if err != nil {
		return err
	}

	if !process.Alive(state.PID) {
		// Nothing to signal; make the stored phase honest.
		if state.Phase != instance.PhaseStopped || state.PID != 0 {
			state.Phase = instance.PhaseStopped
			state.PID = 0
			if err := s.SaveState(inst.Name, state); err != nil {
				return err
			}
		}
		fmt.Fprintf(stdout, "%s is not running\n", inst.Name)
		return nil
	}

	// Record the intent first: the supervisor reads it to tell a requested stop
	// apart from a crash, even when the server exits with a signal.
	state.Phase = instance.PhaseStopping
	if err := s.SaveState(inst.Name, state); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "stopping %s (pid %d), waiting up to %s\n", inst.Name, state.PID, stopGrace)
	if err := process.Stop(state.PID, stopGrace); err != nil {
		return fmt.Errorf("stop instance %q: %w", inst.Name, err)
	}

	// The supervisor records the same phase; writing here keeps `stop` honest
	// when the supervisor was killed together with its terminal.
	state.Phase = instance.PhaseStopped
	state.PID = 0
	state.ExitCode = -1
	if err := s.SaveState(inst.Name, state); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "%s stopped\n", inst.Name)
	return nil
}

// runServe serves the HTTP API and the embedded panel until ctx is cancelled.
func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("serve", stderr)
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("serve takes no arguments: %w", ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *addr, err)
	}

	server := &http.Server{
		Handler:           httpapi.New(s, apiControl{}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	fmt.Fprintf(stdout, "serving http://%s\npress Ctrl+C to stop\n", listener.Addr())

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	// Let in-flight requests finish, then drop the log streams still open.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close() // a stream that outlives the grace period is cut here
	}
	return nil
}

// apiControl adapts the CLI lifecycle to the HTTP API.
type apiControl struct{}

// Start launches one instance in the background and waits until it runs.
func (apiControl) Start(name string) error {
	s, inst, err := loadInstance(name)
	if err != nil {
		return err
	}
	_, logPath, err := prepareStart(s, inst)
	if err != nil {
		return err
	}
	return startDetached(s, inst, logPath, io.Discard)
}

// Stop asks one instance to exit and waits for it.
func (apiControl) Stop(name string) error {
	s, inst, err := loadInstance(name)
	if err != nil {
		return err
	}
	return stopInstance(s, inst, io.Discard)
}

// runBackup archives the data directory of one instance.
func runBackup(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("backup", stderr)
	keep := fs.Int("keep", 5, "backups to keep")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("backup needs exactly one instance name: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	if err := requireStopped(s, inst, "backup"); err != nil {
		return err
	}
	layout := s.Layout(inst.Name)
	if err := os.MkdirAll(layout.Backups, 0o755); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}

	path := filepath.Join(layout.Backups, time.Now().Format("2006-01-02_15-04-05")+".tar.gz")
	res, err := backup.Create(path, layout.Data)
	if err != nil {
		return fmt.Errorf("backup instance %q: %w", inst.Name, err)
	}

	removed, err := backup.Prune(layout.Backups, *keep)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "backed up %s: %s (%d files, %s)\n", inst.Name, res.Path, res.Files, humanBytes(res.Bytes))
	for _, name := range removed {
		fmt.Fprintf(stdout, "removed old backup %s\n", name)
	}
	return nil
}

// runRestore replaces the data directory of one instance from a backup.
func runRestore(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("restore", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("restore needs an instance name and an archive: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	if err := requireStopped(s, inst, "restore"); err != nil {
		return err
	}
	layout := s.Layout(inst.Name)

	archivePath, err := resolveArchive(layout.Backups, fs.Arg(1))
	if err != nil {
		return err
	}

	// Keep the current world before replacing it, so a bad archive is recoverable.
	safety := ""
	if _, err := os.Stat(layout.Data); err == nil {
		safety = filepath.Join(layout.Backups, "pre-restore_"+time.Now().Format("2006-01-02_15-04-05")+".tar.gz")
		if _, err := backup.Create(safety, layout.Data); err != nil {
			return fmt.Errorf("backup the current data first: %w", err)
		}
	}

	if err := backup.Restore(archivePath, layout.Data); err != nil {
		return fmt.Errorf("restore instance %q: %w", inst.Name, err)
	}

	fmt.Fprintf(stdout, "restored %s from %s\n", inst.Name, archivePath)
	if safety != "" {
		fmt.Fprintf(stdout, "previous data saved as %s\n", safety)
	}
	return nil
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

// requireStopped refuses an operation that needs exclusive access to the data.
func requireStopped(s *store.Store, inst instance.Instance, action string) error {
	state, err := s.LoadState(inst.Name)
	if err != nil {
		return err
	}
	if state.Running() && process.Alive(state.PID) {
		return fmt.Errorf("%s instance %q: %w (pid %d), stop it first", action, inst.Name, ErrRunning, state.PID)
	}
	return nil
}

// resolveArchive accepts a path or a file name inside the backup directory.
func resolveArchive(dir, arg string) (string, error) {
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		return arg, nil
	}
	candidate := filepath.Join(dir, arg)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}
	return "", fmt.Errorf("archive %q not found", arg)
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

// runLogs prints or follows the newest log of one instance.
func runLogs(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("logs", stderr)
	follow := fs.Bool("f", false, "follow the log until Ctrl+C")
	lines := fs.Int("n", 100, "lines to print from the end")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("logs needs exactly one instance name: %w", ErrUsage)
	}

	s, err := openStore()
	if err != nil {
		return err
	}
	inst, err := s.Load(fs.Arg(0))
	if err != nil {
		return err
	}

	path, err := logs.Latest(s.Layout(inst.Name).Logs)
	if errors.Is(err, logs.ErrNoLogs) {
		fmt.Fprintf(stdout, "no logs yet for %s, start it first\n", inst.Name)
		return nil
	}
	if err != nil {
		return err
	}

	if *follow {
		return logs.Follow(ctx, stdout, path, *lines)
	}
	return logs.Tail(stdout, path, *lines)
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

	// A running phase with a dead process means the server did not shut down
	// cleanly; a stopping phase whose process is gone finished its stop.
	live := process.Alive(state.PID)
	state = state.Resolve(live)
	alive := state.Running() && live

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

	// The live value wins over the state, which only knows the last run.
	if driver, err := games.Lookup(inst.Game); err == nil {
		label, value := driver.Runner.Join(inst)
		if value == "" {
			value = state.GameID
		}
		if value != "" {
			rows = append(rows, [2]string{label, value})
		}
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
