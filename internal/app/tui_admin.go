package app

import (
	"fmt"
	"strings"

	"github.com/syberfoxx/gamehost/internal/tui"
)

// lobbyAdmins lists the admins of one instance.
func lobbyAdmins(name string) ([]tui.Admin, error) {
	_, inst, err := loadInstance(name)
	if err != nil {
		return nil, err
	}

	list, err := listAdmins(inst)
	if err != nil {
		return nil, err
	}

	out := make([]tui.Admin, 0, len(list))
	for _, entry := range list {
		out = append(out, tui.Admin{
			SteamID:    entry.ID,
			Name:       entry.Name,
			Privileges: entry.Privileges,
		})
	}
	return out, nil
}

// lobbyAdminAdd grants admin rights from the lobby form.
func lobbyAdminAdd(name, id, displayName string, limited bool) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	id = strings.TrimSpace(id)
	added, err := grantAdmin(s, inst, id, displayName, limited)
	if err != nil {
		return "", fmt.Errorf("admin add: %w", err)
	}

	privileges := "full"
	if limited {
		privileges = "limited"
	}
	action := "updated"
	if added {
		action = "added"
	}
	return fmt.Sprintf("%s admin %s (%s, %s)", action, id, privileges, inst.Name), nil
}

// lobbyAdminRemove drops admin rights from one player.
func lobbyAdminRemove(name, id string) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	id = strings.TrimSpace(id)
	if err := revokeAdmin(s, inst, id); err != nil {
		return "", fmt.Errorf("admin remove: %w", err)
	}
	return fmt.Sprintf("removed admin %s from %s", id, inst.Name), nil
}
