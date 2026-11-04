// Package games lists the game drivers the manager supports.
package games

import (
	"errors"
	"fmt"

	"github.com/syberfoxx/gamehost/internal/corekeeper"
	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/projectzomboid"
)

// ErrUnsupported reports a game without a driver.
var ErrUnsupported = errors.New("unsupported game")

// All returns one registration per supported game.
func All() []game.Registration {
	return []game.Registration{
		{
			Game:      instance.GameCoreKeeper,
			Installer: corekeeper.Installer{},
			Runner:    corekeeper.Runner{},
		},
		{
			Game:      instance.GameProjectZomboid,
			Installer: projectzomboid.Installer{},
			Runner:    projectzomboid.Runner{},
		},
	}
}

// Lookup returns the driver of one game.
func Lookup(name string) (game.Registration, error) {
	for _, reg := range All() {
		if reg.Game == name {
			return reg, nil
		}
	}
	return game.Registration{}, fmt.Errorf("%q: %w", name, ErrUnsupported)
}
