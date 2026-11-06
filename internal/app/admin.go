package app

import (
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/syberfoxx/gamehost/internal/admins"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// runAdmin lists, adds or removes the admins of a Core Keeper instance.
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

// adminFile returns the Admins.json path of one instance.
func adminFile(inst instance.Instance) string {
	return filepath.Join(inst.DataDir, "Admins.json")
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
	fmt.Fprintln(tw, "STEAM ID\tPRIVILEGES\tNAME")
	for _, entry := range list {
		fmt.Fprintf(tw, "%d\t%s\t%s\n", entry.SteamID, admins.PrivilegeLabel(entry.Privileges), entry.Name)
	}
	return tw.Flush()
}

// listAdmins reads the admin list of one instance.
func listAdmins(inst instance.Instance) ([]admins.Entry, error) {
	file, err := admins.Load(adminFile(inst))
	if err != nil {
		return nil, err
	}
	return file.AdminList, nil
}

// runAdminAdd grants admin rights to one Steam account.
func runAdminAdd(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("admin add", stderr)
	name := fs.String("name", "", "player name shown in the admin list")
	limited := fs.Bool("limited", false, "grant limited privileges instead of full")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("admin add needs an instance name and a steam id: %w", ErrUsage)
	}

	steamID, err := admins.ParseSteamID(fs.Arg(1))
	if err != nil {
		return fmt.Errorf("admin add: %w: %w", ErrUsage, err)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}

	privileges := admins.Full
	if *limited {
		privileges = admins.Limited
	}

	added, err := grantAdmin(s, inst, steamID, *name, privileges)
	if err != nil {
		return err
	}

	action := "updated"
	if added {
		action = "added"
	}
	fmt.Fprintf(stdout, "%s admin %d (%s, %s)\n", action, steamID, admins.PrivilegeLabel(privileges), inst.Name)
	return nil
}

// grantAdmin adds or updates one admin while the server is stopped.
func grantAdmin(s *store.Store, inst instance.Instance, steamID uint64, name string, privileges int) (bool, error) {
	release, err := requireStopped(s, inst, "admin add")
	if err != nil {
		return false, err
	}
	defer release()

	file, err := admins.Load(adminFile(inst))
	if err != nil {
		return false, err
	}
	added, err := file.Add(steamID, name, privileges)
	if err != nil {
		return false, err
	}
	if err := file.Save(adminFile(inst)); err != nil {
		return false, err
	}
	return added, nil
}

// runAdminRemove drops admin rights from one Steam account.
func runAdminRemove(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("admin remove", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("admin remove needs an instance name and a steam id: %w", ErrUsage)
	}

	steamID, err := admins.ParseSteamID(fs.Arg(1))
	if err != nil {
		return fmt.Errorf("admin remove: %w: %w", ErrUsage, err)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	if err := revokeAdmin(s, inst, steamID); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "removed admin %d from %s\n", steamID, inst.Name)
	return nil
}

// revokeAdmin removes one admin while the server is stopped.
func revokeAdmin(s *store.Store, inst instance.Instance, steamID uint64) error {
	release, err := requireStopped(s, inst, "admin remove")
	if err != nil {
		return err
	}
	defer release()

	file, err := admins.Load(adminFile(inst))
	if err != nil {
		return err
	}
	if !file.Remove(steamID) {
		return fmt.Errorf("%d is not an admin of %q", steamID, inst.Name)
	}
	return file.Save(adminFile(inst))
}
