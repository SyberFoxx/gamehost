package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/syberfoxx/gamehost/internal/games"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/logs"
	"github.com/syberfoxx/gamehost/internal/process"
	"github.com/syberfoxx/gamehost/internal/store"
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

// runLobby opens the interactive lobby until the user quits.
func runLobby(ctx context.Context, stdout io.Writer) error {
	return tui.Run(ctx, tui.Options{
		In:  os.Stdin,
		Out: stdout,
		Backend: tui.Backend{
			List:   lobbyItems,
			Start:  lobbyStart,
			Stop:   lobbyStop,
			Backup: lobbyBackup,
			Logs:   lobbyLogs,
		},
	})
}

// lobbyItems resolves every instance for the lobby.
func lobbyItems() ([]tui.Item, error) {
	s, err := openStore()
	if err != nil {
		return nil, err
	}

	instances, err := s.List()
	if err != nil {
		return nil, err
	}

	items := make([]tui.Item, 0, len(instances))
	for _, inst := range instances {
		item, err := lobbyItem(s, inst)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// lobbyItem resolves one instance, reconciling the stored phase with the
// liveness of its process exactly like the status command does.
func lobbyItem(s *store.Store, inst instance.Instance) (tui.Item, error) {
	state, err := s.LoadState(inst.Name)
	if err != nil {
		return tui.Item{}, err
	}

	live := process.Alive(state.PID)
	state = state.Resolve(live)

	item := tui.Item{Name: inst.Name, Game: inst.Game, Phase: string(state.Phase)}
	if driver, err := games.Lookup(inst.Game); err == nil {
		if driver.Label != "" {
			item.Game = driver.Label
		}
		// The live session id only exists once the server wrote it; the state
		// keeps the last one, the same fallback status uses.
		label, value := driver.Runner.Join(inst)
		if value == "" {
			value = state.GameID
		}
		if value == "" {
			value = "(set on first start)"
		}
		item.JoinLabel, item.JoinValue = label, value
	}
	if state.Running() && live {
		item.PID = state.PID
		item.StartedAt = state.StartedAt
	}
	return item, nil
}

// lobbyStart hands the instance to the detached supervisor, the same path
// start --detach uses. It blocks until the supervisor reports a pid.
func lobbyStart(_ context.Context, name string) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	_, logPath, err := prepareStart(s, inst)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := startDetached(s, inst, logPath, &out); err != nil {
		return "", err
	}
	return firstLine(out.String()), nil
}

// lobbyStop asks the server to save and exit.
func lobbyStop(_ context.Context, name string) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if err := stopInstance(s, inst, &out); err != nil {
		return "", err
	}
	return lastLine(out.String()), nil
}

// lobbyBackup archives the world of a stopped instance.
func lobbyBackup(_ context.Context, name string) (string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	if _, err := backupInstance(s, inst, defaultBackupKeep, &out); err != nil {
		return "", err
	}
	return firstLine(out.String()), nil
}

// lobbyLogs reads the newest log of one instance.
func lobbyLogs(name string, lines int) ([]string, error) {
	s, inst, err := loadInstance(name)
	if err != nil {
		return nil, err
	}

	path, err := logs.Latest(s.Layout(inst.Name).Logs)
	if errors.Is(err, logs.ErrNoLogs) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out bytes.Buffer
	if err := logs.Tail(&out, path, lines); err != nil {
		return nil, err
	}
	if out.Len() == 0 {
		return nil, nil
	}
	return strings.Split(strings.TrimRight(out.String(), "\n"), "\n"), nil
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
