package projectzomboid

import (
	"path/filepath"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// ConsoleFile is the fifo the server reads console commands from.
const ConsoleFile = "gamehost.control"

// ConsolePath returns the control file of one instance, inside the cache
// directory the launch already owns.
func (Runner) ConsolePath(inst instance.Instance) string {
	return filepath.Join(inst.DataDir, ConsoleFile)
}

// StopCommands returns the lines that end the server cleanly. The PZ devs
// document save first and quit second, and discourage a signal instead.
func (Runner) StopCommands(instance.Instance) []string {
	return []string{"save", "quit"}
}
