package app

import (
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"

	"github.com/syberfoxx/gamehost/internal/admins"
	"github.com/syberfoxx/gamehost/internal/instance"
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
	file, err := admins.Load(adminFile(inst))
	if err != nil {
		return err
	}
	if len(file.AdminList) == 0 {
		fmt.Fprintf(stdout, "%s has no admins yet\n", inst.Name)
		return nil
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "STEAM ID\tPRIVILEGES\tNAME")
	for _, entry := range file.AdminList {
		fmt.Fprintf(tw, "%d\t%s\t%s\n", entry.SteamID, admins.PrivilegeLabel(entry.Privileges), entry.Name)
	}
	return tw.Flush()
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
	release, err := requireStopped(s, inst, "admin add")
	if err != nil {
		return err
	}
	defer release()

	privileges := admins.Full
	if *limited {
		privileges = admins.Limited
	}

	file, err := admins.Load(adminFile(inst))
	if err != nil {
		return err
	}
	added, err := file.Add(steamID, *name, privileges)
	if err != nil {
		return err
	}
	if err := file.Save(adminFile(inst)); err != nil {
		return err
	}

	action := "updated"
	if added {
		action = "added"
	}
	fmt.Fprintf(stdout, "%s admin %d (%s, %s)\n", action, steamID, admins.PrivilegeLabel(privileges), inst.Name)
	return nil
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
	if err := file.Save(adminFile(inst)); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "removed admin %d from %s\n", steamID, inst.Name)
	return nil
}
