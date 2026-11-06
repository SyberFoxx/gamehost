package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/games"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/logs"
	"github.com/syberfoxx/gamehost/internal/process"
	"github.com/syberfoxx/gamehost/internal/store"
)

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

	if _, err := logs.Prune(s.Layout(inst.Name).Logs, logRetention); err != nil {
		return err
	}
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

	// A driver that keeps its settings in files of its own writes them here,
	// before the launch, so the server never reads a stale cluster.
	if err := prepareRuntime(driver, inst); err != nil {
		return game.Launch{}, "", err
	}

	// Validate the launch contract before a process exists.
	launch, err := driver.Runner.Launch(inst)
	if err != nil {
		return game.Launch{}, "", err
	}
	logPath := filepath.Join(s.Layout(inst.Name).Logs, time.Now().Format("2006-01-02_15-04-05")+".log")
	return launch, logPath, nil
}

// prepareRuntime lets a driver write the files its server reads at startup.
func prepareRuntime(driver game.Registration, inst instance.Instance) error {
	preparer, ok := driver.Runner.(game.Preparer)
	if !ok {
		return nil
	}
	if err := preparer.Prepare(inst); err != nil {
		return fmt.Errorf("start instance %q: %w", inst.Name, err)
	}
	return nil
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

	if _, err := logs.Prune(s.Layout(inst.Name).Logs, logRetention); err != nil {
		return err
	}

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
	// The lock closes the window between the stopped check and the launch, so
	// two servers can never share one data directory.
	lock, err := s.Lock(inst.Name)
	if err != nil {
		if errors.Is(err, store.ErrLocked) {
			return fmt.Errorf("start instance %q: %w", inst.Name, ErrRunning)
		}
		return fmt.Errorf("start instance %q: %w", inst.Name, err)
	}
	defer func() { _ = lock.Release() }()

	startSpec := process.Spec{
		Path: launch.Path,
		Args: launch.Args,
		Dir:  launch.Dir,
		Out:  out,
	}
	server, err := process.Start(startSpec)
	if err != nil {
		return fmt.Errorf("start instance %q: %w", inst.Name, err)
	}

	// Extra shards join the primary process group, so the pid in state.json and
	// every stop signal cover the whole instance, not only the surface world.
	servers := []*process.Server{server}
	for _, extra := range launch.Extra {
		shard, err := process.StartJoined(process.Spec{
			Path: extra.Path,
			Args: extra.Args,
			Dir:  extra.Dir,
			Out:  out,
		}, server.PID())
		if err != nil {
			_ = process.Stop(server.PID(), stopGrace)
			return fmt.Errorf("start instance %q: %w", inst.Name, err)
		}
		servers = append(servers, shard)
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
	for _, shard := range servers[1:] {
		fmt.Fprintf(out, "supervising the caves shard (pid %d)\n", shard.PID())
	}

	// One shard dying takes the instance down: a surface world without caves
	// would silently swallow every portal, so stop the rest and report it.
	results := make(chan process.Result, len(servers))
	for _, shard := range servers {
		go func(shard *process.Server) { results <- shard.WaitContext(ctx, stopGrace) }(shard)
	}
	res := <-results
	for range servers[1:] {
		if anyAlive(servers) {
			_ = process.Stop(server.PID(), stopGrace)
		}
		res = mergeShardResults(res, <-results)
	}

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

// anyAlive reports whether any shard of an instance still runs.
func anyAlive(servers []*process.Server) bool {
	for _, shard := range servers {
		if process.Alive(shard.PID()) {
			return true
		}
	}
	return false
}

// mergeShardResults folds the exit of one shard into the instance result.
// A shard that failed without being asked to stop is the interesting one.
func mergeShardResults(res, other process.Result) process.Result {
	if other.Duration > res.Duration {
		res.Duration = other.Duration
	}
	if !other.Stopped && other.ExitCode != 0 {
		res.ExitCode = other.ExitCode
		res.Stopped = false
	}
	return res
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
	// The parent prepared the cluster before re-execing, but the hidden command
	// must also stand on its own when it is the entry point.
	if err := prepareRuntime(driver, inst); err != nil {
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

	// The Master is gone, but a second shard may still be exiting. Waiting for
	// the group keeps the lock free by the time stop returns, so the next
	// command never races the supervisor.
	deadline := time.Now().Add(stopGrace)
	for process.GroupAlive(state.PID) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
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
