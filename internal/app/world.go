package app

import (
	"fmt"
	"io"

	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/games"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runWorld dispatches the world lifecycle subcommands.
func runWorld(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("world needs a subcommand: recreate or rollback: %w", ErrUsage)
	}
	switch args[0] {
	case "recreate":
		return runWorldRecreate(args[1:], stdout, stderr)
	case "rollback":
		return runWorldRollback(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown world subcommand %q (want recreate or rollback): %w", args[0], ErrUsage)
	}
}

// runWorldRecreate discards the generated world of a stopped instance. The
// explicit confirmation is deliberate: the next start creates a new map and
// nothing but the safety archive can bring the old one back.
func runWorldRecreate(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("world recreate", stderr)
	yes := fs.Bool("yes", false, "confirm the world recreation")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("world recreate needs exactly one instance name: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	if !*yes {
		return fmt.Errorf("world recreate %q removes the generated world; add --yes to confirm: %w", inst.Name, ErrUsage)
	}
	return recreateWorld(s, inst, stdout)
}

// recreateWorld stops short of the server's configuration and saves a safety
// archive before it deletes the world data.
func recreateWorld(s *store.Store, inst instance.Instance, stdout io.Writer) error {
	release, err := requireStopped(s, inst, "world recreate")
	if err != nil {
		return err
	}
	defer release()

	driver, err := games.Lookup(inst.Game)
	if err != nil {
		return err
	}
	resetter, ok := driver.Runner.(game.WorldResetter)
	if !ok {
		return fmt.Errorf("%s does not support world recreate: %w", driver.Label, ErrUsage)
	}

	safety, err := archiveData(s, inst, "pre-recreate_")
	if err != nil {
		return err
	}
	if err := resetter.ResetWorld(inst); err != nil {
		return fmt.Errorf("recreate world of %q: %w", inst.Name, err)
	}

	fmt.Fprintf(stdout, "recreated the world of %s; the next start generates a new one\n", inst.Name)
	if safety.Path != "" {
		fmt.Fprintf(stdout, "previous data saved as %s\n", safety.Path)
	}
	return nil
}

// runWorldRollback is the named entry point to the existing restore path.
// DST's own snapshot layout is not documented, so gamehost restores a backup
// it made itself instead of claiming to call the game's c_rollback command.
func runWorldRollback(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("world rollback", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("world rollback needs an instance name and an archive: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	return restoreInstance(s, inst, fs.Arg(1), stdout)
}
