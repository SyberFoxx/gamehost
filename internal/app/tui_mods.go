package app

import (
	"fmt"

	"github.com/syberfoxx/gamehost/internal/tui"
)

// lobbyMods lists the workshop items and mods of one instance.
func lobbyMods(name string) (tui.Mods, error) {
	_, inst, err := loadInstance(name)
	if err != nil {
		return tui.Mods{}, err
	}
	mods, err := listMods(inst)
	if err != nil {
		return tui.Mods{}, err
	}
	return tui.Mods{Workshop: mods.Workshop, Enabled: mods.Enabled}, nil
}

// lobbyModAdd adds one workshop item or mod id from the lobby form.
func lobbyModAdd(name, id string, workshop bool) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}
	added, err := addMod(s, inst, id, workshop)
	if err != nil {
		return "", fmt.Errorf("mods add: %w", err)
	}
	kind := "mod"
	if workshop {
		kind = "workshop item"
	}
	action := "added"
	if !added {
		action = "kept"
	}
	return fmt.Sprintf("%s %s %s on %s", action, kind, id, inst.Name), nil
}

// lobbyModRemove drops one workshop item or mod id.
func lobbyModRemove(name, id string, workshop bool) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}
	if err := removeMod(s, inst, id, workshop); err != nil {
		return "", fmt.Errorf("mods remove: %w", err)
	}
	kind := "mod"
	if workshop {
		kind = "workshop item"
	}
	return fmt.Sprintf("removed %s %s from %s", kind, id, inst.Name), nil
}
