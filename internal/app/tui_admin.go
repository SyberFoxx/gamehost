package app

import (
	"fmt"
	"strconv"

	"github.com/syberfoxx/gamehost/internal/admins"
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
			SteamID:    strconv.FormatUint(entry.SteamID, 10),
			Name:       entry.Name,
			Privileges: admins.PrivilegeLabel(entry.Privileges),
		})
	}
	return out, nil
}

// lobbyAdminAdd grants admin rights from the lobby form.
func lobbyAdminAdd(name, steamID, displayName string, limited bool) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	id, err := admins.ParseSteamID(steamID)
	if err != nil {
		return "", fmt.Errorf("admin add: %w: %w", ErrUsage, err)
	}

	privileges := admins.Full
	if limited {
		privileges = admins.Limited
	}

	added, err := grantAdmin(s, inst, id, displayName, privileges)
	if err != nil {
		return "", err
	}

	action := "updated"
	if added {
		action = "added"
	}
	return fmt.Sprintf("%s admin %d (%s, %s)", action, id, admins.PrivilegeLabel(privileges), inst.Name), nil
}

// lobbyAdminRemove drops admin rights from one Steam account.
func lobbyAdminRemove(name, steamID string) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	id, err := admins.ParseSteamID(steamID)
	if err != nil {
		return "", fmt.Errorf("admin remove: %w: %w", ErrUsage, err)
	}
	if err := revokeAdmin(s, inst, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("removed admin %d from %s", id, inst.Name), nil
}
