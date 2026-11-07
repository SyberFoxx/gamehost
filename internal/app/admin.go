package app

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/games"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runAdmin lists, adds or removes the admins of one instance.
func runAdmin(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("admin needs a subcommand: list, add or remove: %w", ErrUsage)
	}
	switch args[0] {
	case "list":
		return runAdminList(args[1:], stdout, stderr)
	case "add":
		return runAdminAdd(args[1:], stdout, stderr)
	case "remove":
		return runAdminRemove(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown admin subcommand %q (want list, add or remove): %w", args[0], ErrUsage)
	}
}

// adminStore resolves the admin file handler of one game.
func adminStore(inst instance.Instance) (game.AdminStore, error) {
	driver, err := games.Lookup(inst.Game)
	if err != nil {
		return nil, err
	}
	store, ok := driver.Runner.(game.AdminStore)
	if !ok {
		return nil, fmt.Errorf("%s does not support admins yet", driver.Label)
	}
	return store, nil
}

// runAdminList prints the current admins of one instance.
func runAdminList(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("admin list", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("admin list needs exactly one instance name: %w", ErrUsage)
	}

	_, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	list, err := listAdmins(inst)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintf(stdout, "%s has no admins yet\n", inst.Name)
		return nil
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tPRIVILEGES\tNAME")
	for _, entry := range list {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", entry.ID, entry.Privileges, entry.Name)
	}
	return tw.Flush()
}

// listAdmins reads the admin list of one instance.
func listAdmins(inst instance.Instance) ([]game.Admin, error) {
	store, err := adminStore(inst)
	if err != nil {
		return nil, err
	}
	return store.Admins(inst)
}

// runAdminAdd grants admin rights to one player.
func runAdminAdd(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("admin add", stderr)
	name := fs.String("name", "", "player name shown in the admin list (corekeeper only)")
	limited := fs.Bool("limited", false, "grant limited privileges instead of full (corekeeper only)")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("admin add needs an instance name and a player id: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	if err := rejectForeignFlags("admin add", inst.Game, setFlags(fs)); err != nil {
		return err
	}

	id := strings.TrimSpace(fs.Arg(1))
	added, err := grantAdmin(s, inst, id, *name, *limited)
	if err != nil {
		return fmt.Errorf("admin add: %w", err)
	}

	privileges := "full"
	if *limited {
		privileges = "limited"
	}
	action := "updated"
	if added {
		action = "added"
	}
	fmt.Fprintf(stdout, "%s admin %s (%s, %s)\n", action, id, privileges, inst.Name)
	return nil
}

// grantAdmin adds or updates one admin while the server is stopped.
func grantAdmin(s *store.Store, inst instance.Instance, id, name string, limited bool) (bool, error) {
	store, err := adminStore(inst)
	if err != nil {
		return false, err
	}
	release, err := requireStopped(s, inst, "admin add")
	if err != nil {
		return false, err
	}
	defer release()

	return store.AddAdmin(inst, id, name, limited)
}

// runAdminRemove drops admin rights from one player.
func runAdminRemove(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("admin remove", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("admin remove needs an instance name and a player id: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(1))
	if err := revokeAdmin(s, inst, id); err != nil {
		return fmt.Errorf("admin remove: %w", err)
	}

	fmt.Fprintf(stdout, "removed admin %s from %s\n", id, inst.Name)
	return nil
}

// revokeAdmin removes one admin while the server is stopped.
func revokeAdmin(s *store.Store, inst instance.Instance, id string) error {
	store, err := adminStore(inst)
	if err != nil {
		return err
	}
	release, err := requireStopped(s, inst, "admin remove")
	if err != nil {
		return err
	}
	defer release()

	return store.RemoveAdmin(inst, id)
}
