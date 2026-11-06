package app

import (
	"fmt"

	"github.com/syberfoxx/gamehost/internal/doctor"
	"github.com/syberfoxx/gamehost/internal/games"
	"github.com/syberfoxx/gamehost/internal/home"
	"github.com/syberfoxx/gamehost/internal/tui"
)

// lobbyDoctor runs the environment checks for the lobby.
func lobbyDoctor() (tui.Report, error) {
	root, err := home.Dir()
	if err != nil {
		return tui.Report{}, fmt.Errorf("resolve data directory: %w", err)
	}

	report := doctor.Run(root)
	checks := make([]tui.Check, 0, len(report.Checks))
	for _, check := range report.Checks {
		checks = append(checks, tui.Check{
			Name:   check.Name,
			Status: check.Status.String(),
			Detail: check.Detail,
		})
	}
	return tui.Report{
		Root:    report.Root,
		Healthy: report.Healthy(),
		Checks:  checks,
	}, nil
}

// lobbyGames lists the supported games with their display labels.
func lobbyGames() []tui.GameChoice {
	registrations := games.All()
	out := make([]tui.GameChoice, 0, len(registrations))
	for _, registration := range registrations {
		out = append(out, tui.GameChoice{
			Name:  registration.Game,
			Label: registration.Label,
		})
	}
	return out
}
