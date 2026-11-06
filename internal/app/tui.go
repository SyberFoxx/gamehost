package app

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/syberfoxx/gamehost/internal/tui"
)

// wantsLobby reports whether this invocation should open the interactive lobby.
// A pipe, a file or a test buffer still gets the usage text: only a real
// terminal is a person who can press arrow keys.
func wantsLobby(args []string, stdout io.Writer, stdin *os.File) bool {
	if len(args) != 0 || stdin == nil {
		return false
	}
	file, ok := stdout.(*os.File)
	return ok && isTerminal(file) && isTerminal(stdin)
}

// isTerminal reports whether f is a terminal. A character-device check is not
// enough: /dev/null is one too, and pointing the lobby at it would hide the UI.
func isTerminal(f *os.File) bool {
	return term.IsTerminal(f.Fd())
}

// runLobby opens the interactive lobby until the user quits. The child context
// gives the web panel the same lifetime as the lobby, even when the user quits
// from the root and never closes the panel screen.
func runLobby(ctx context.Context, stdout io.Writer) error {
	lobbyCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	return tui.Run(lobbyCtx, tui.Options{
		In:      os.Stdin,
		Out:     stdout,
		Backend: lobbyBackend(),
	})
}

// lobbyBackend wires every capability of the command layer into the lobby.
// Each group is a set of functions, so a screen can be tested with only the
// group it uses and the TUI keeps importing nothing from this project.
func lobbyBackend() tui.Backend {
	return tui.Backend{
		Instances: tui.InstanceBackend{
			List:   lobbyItems,
			Show:   lobbyShow,
			Create: lobbyCreate,
			Config: lobbyConfig,
			Update: lobbyUpdate,
		},
		Server: tui.ServerBackend{
			Start:       lobbyStart,
			Stop:        lobbyStop,
			Backup:      lobbyBackup,
			Restore:     lobbyRestore,
			ImportWorld: lobbyImportWorld,
			Backups:     lobbyBackups,
		},
		Logs: tui.LogBackend{
			Follow: lobbyFollow,
		},
		Files: tui.FileBackend{
			Install: func(ctx context.Context, name string) <-chan tui.Event {
				return lobbyDownload(ctx, "install", name)
			},
			Update: func(ctx context.Context, name string) <-chan tui.Event {
				return lobbyDownload(ctx, "update", name)
			},
		},
		Admins: tui.AdminBackend{
			List:   lobbyAdmins,
			Add:    lobbyAdminAdd,
			Remove: lobbyAdminRemove,
		},
		Mods: tui.ModsBackend{
			List:   lobbyMods,
			Add:    lobbyModAdd,
			Remove: lobbyModRemove,
		},
		Service: tui.ServiceBackend{
			Status:    lobbyServiceStatus,
			Install:   lobbyServiceInstall,
			Uninstall: lobbyServiceUninstall,
		},
		Env: tui.EnvBackend{
			Doctor:  lobbyDoctor,
			Games:   lobbyGames,
			Version: lobbyVersion,
		},
		Panel: tui.PanelBackend{
			Start: servePanel,
		},
	}
}

// firstLine returns the first non-empty line, the useful half of a report.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// lastLine returns the last non-empty line, where a stop reports its outcome.
func lastLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
