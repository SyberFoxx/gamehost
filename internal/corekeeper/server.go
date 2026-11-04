package corekeeper

import (
	"fmt"
	"strconv"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// Args returns the launch arguments for an instance.
func Args(inst instance.Instance) ([]string, error) {
	mode := inst.World.Mode.ServerName()
	if mode == "" {
		return nil, fmt.Errorf("unsupported world mode %q", inst.World.Mode)
	}

	args := []string{
		"-batchmode",
		"-logfile", "/dev/stdout",
		"-datapath", inst.DataDir,
		"-world", strconv.Itoa(inst.World.Index),
		"-worldname", inst.World.Name,
		"-worldmode", mode,
		"-maxplayers", strconv.Itoa(inst.MaxPlayers),
	}
	if inst.World.Seed != "" {
		args = append(args, "-worldseed", inst.World.Seed)
	}
	if inst.GameID != "" {
		args = append(args, "-gameid", inst.GameID)
	}
	if inst.Network.Mode == instance.NetworkDirect {
		args = append(args, "-port", strconv.Itoa(inst.Network.Port))
		if inst.Network.Password != "" {
			args = append(args, "-password", inst.Network.Password)
		}
	}
	return args, nil
}
