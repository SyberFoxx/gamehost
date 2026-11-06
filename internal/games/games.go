// Package games lists the game drivers the manager supports.
package games

import (
	"errors"
	"fmt"

	"github.com/syberfoxx/gamehost/internal/corekeeper"
	"github.com/syberfoxx/gamehost/internal/dontstarvetogether"
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
			Label:     "Core Keeper",
			Installer: corekeeper.Installer{},
			Runner:    corekeeper.Runner{},
		},
		{
			Game:      instance.GameProjectZomboid,
			Label:     "Project Zomboid",
			Installer: projectzomboid.Installer{},
			Runner:    projectzomboid.Runner{},
		},
		{
			Game:      instance.GameDontStarveTogether,
			Label:     "Don't Starve Together",
			Installer: dontstarvetogether.Installer{},
			Runner:    dontstarvetogether.Runner{},
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
