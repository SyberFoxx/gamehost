package corekeeper

import (
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/syberfoxx/gamehost/internal/admins"
	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
)

// AdminFile returns the Admins.json path of one instance.
func AdminFile(inst instance.Instance) string {
	return filepath.Join(inst.DataDir, "Admins.json")
}

// Admins reads the admin list as the CLI and the lobby show it.
func (Runner) Admins(inst instance.Instance) ([]game.Admin, error) {
	file, err := admins.Load(AdminFile(inst))
	if err != nil {
		return nil, err
	}
	list := make([]game.Admin, 0, len(file.AdminList))
	for _, entry := range file.AdminList {
		list = append(list, game.Admin{
			ID:         strconv.FormatUint(entry.SteamID, 10),
			Name:       entry.Name,
			Privileges: admins.PrivilegeLabel(entry.Privileges),
		})
	}
	return list, nil
}

// AddAdmin grants admin rights to one Steam account.
func (Runner) AddAdmin(inst instance.Instance, id, name string, limited bool) (bool, error) {
	steamID, err := admins.ParseSteamID(id)
	if err != nil {
		return false, err
	}
	privileges := admins.Full
	if limited {
		privileges = admins.Limited
	}

	file, err := admins.Load(AdminFile(inst))
	if err != nil {
		return false, err
	}
	added, err := file.Add(steamID, name, privileges)
	if err != nil {
		return false, err
	}
	if err := file.Save(AdminFile(inst)); err != nil {
		return false, err
	}
	return added, nil
}

// RemoveAdmin drops admin rights from one Steam account.
func (Runner) RemoveAdmin(inst instance.Instance, id string) error {
	steamID, err := admins.ParseSteamID(id)
	if err != nil {
		return err
	}
	file, err := admins.Load(AdminFile(inst))
	if err != nil {
		return err
	}
	if !file.Remove(steamID) {
		return fmt.Errorf("%s is not an admin of %q", id, inst.Name)
	}
	return file.Save(AdminFile(inst))
}
