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

// runMods lists, adds or removes the mods of one instance.
func runMods(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("mods needs a subcommand: list, add or remove: %w", ErrUsage)
	}
	switch args[0] {
	case "list":
		return runModsList(args[1:], stdout, stderr)
	case "add":
		return runModsAdd(args[1:], stdout, stderr)
	case "remove":
		return runModsRemove(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("unknown mods subcommand %q (want list, add or remove): %w", args[0], ErrUsage)
	}
}

// modStore resolves the mod handler of one game.
func modStore(inst instance.Instance) (game.ModStore, error) {
	driver, err := games.Lookup(inst.Game)
	if err != nil {
		return nil, err
	}
	mods, ok := driver.Runner.(game.ModStore)
	if !ok {
		return nil, fmt.Errorf("%s does not support mods yet", driver.Label)
	}
	return mods, nil
}

// runModsList prints the workshop items and the enabled mods.
func runModsList(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("mods list", stderr)
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("mods list needs exactly one instance name: %w", ErrUsage)
	}

	_, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	mods, err := listMods(inst)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "WORKSHOP ITEMS")
	if len(mods.Workshop) == 0 {
		fmt.Fprintln(tw, "  (none)")
	}
	for _, id := range mods.Workshop {
		fmt.Fprintf(tw, "  %s\n", id)
	}
	fmt.Fprintln(tw, "MODS")
	if len(mods.Enabled) == 0 {
		fmt.Fprintln(tw, "  (none)")
	}
	for _, id := range mods.Enabled {
		fmt.Fprintf(tw, "  %s\n", id)
	}
	return tw.Flush()
}

// listMods reads the mod configuration of one instance.
func listMods(inst instance.Instance) (game.Mods, error) {
	store, err := modStore(inst)
	if err != nil {
		return game.Mods{}, err
	}
	return store.Mods(inst)
}

// runModsAdd adds one workshop item or one mod id.
func runModsAdd(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("mods add", stderr)
	workshop := fs.Bool("workshop", false, "the id is a Steam Workshop item, not a mod id")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("mods add needs an instance name and an id: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(1))
	added, err := addMod(s, inst, id, *workshop)
	if err != nil {
		return err
	}

	kind := "mod"
	if *workshop {
		kind = "workshop item"
	}
	action := "added"
	if !added {
		action = "kept"
	}
	fmt.Fprintf(stdout, "%s %s %s on %s\n", action, kind, id, inst.Name)
	return nil
}

// addMod appends one id to the list while the server is stopped.
func addMod(s *store.Store, inst instance.Instance, id string, workshop bool) (bool, error) {
	store, err := modStore(inst)
	if err != nil {
		return false, err
	}
	release, err := requireStopped(s, inst, "mods add")
	if err != nil {
		return false, err
	}
	defer release()

	mods, err := store.Mods(inst)
	if err != nil {
		return false, err
	}
	list := &mods.Enabled
	if workshop {
		list = &mods.Workshop
	}
	for _, existing := range *list {
		if existing == id {
			return false, nil
		}
	}
	*list = append(*list, id)
	if err := store.SaveMods(inst, mods); err != nil {
		return false, err
	}
	return true, nil
}

// runModsRemove drops one workshop item or one mod id.
func runModsRemove(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("mods remove", stderr)
	workshop := fs.Bool("workshop", false, "the id is a Steam Workshop item, not a mod id")
	if err := fs.Parse(reorderFlags(fs, args)); err != nil {
		return usageError(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("mods remove needs an instance name and an id: %w", ErrUsage)
	}

	s, inst, err := loadInstance(fs.Arg(0))
	if err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(1))
	if err := removeMod(s, inst, id, *workshop); err != nil {
		return err
	}

	kind := "mod"
	if *workshop {
		kind = "workshop item"
	}
	fmt.Fprintf(stdout, "removed %s %s from %s\n", kind, id, inst.Name)
	return nil
}

// removeMod drops one id from the list while the server is stopped.
func removeMod(s *store.Store, inst instance.Instance, id string, workshop bool) error {
	store, err := modStore(inst)
	if err != nil {
		return err
	}
	release, err := requireStopped(s, inst, "mods remove")
	if err != nil {
		return err
	}
	defer release()

	mods, err := store.Mods(inst)
	if err != nil {
		return err
	}
	list := &mods.Enabled
	if workshop {
		list = &mods.Workshop
	}
	kept := make([]string, 0, len(*list))
	for _, existing := range *list {
		if existing != id {
			kept = append(kept, existing)
		}
	}
	if len(kept) == len(*list) {
		return fmt.Errorf("%s is not in the list of %q", id, inst.Name)
	}
	*list = kept
	return store.SaveMods(inst, mods)
}
