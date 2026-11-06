package dontstarvetogether

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/syberfoxx/gamehost/internal/atomicfile"
	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
)

// ErrInvalidAdminID reports a value that is not a Klei user id.
var ErrInvalidAdminID = errors.New("invalid Klei user id")

// AdminFile returns the adminlist.txt path of one cluster.
func AdminFile(inst instance.Instance) string {
	return filepath.Join(ClusterDir(inst), "adminlist.txt")
}

// ParseAdminID validates a Klei user id such as KU_QDNly1sC.
func ParseAdminID(value string) (string, error) {
	id := strings.TrimSpace(value)
	if !strings.HasPrefix(id, "KU_") || len(id) < 6 || len(id) > 64 {
		return "", fmt.Errorf("Klei user id %q must look like KU_xxxxxxxx: %w", value, ErrInvalidAdminID)
	}
	for _, r := range id[3:] {
		isAlphanumeric := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !isAlphanumeric {
			return "", fmt.Errorf("Klei user id %q must be KU_ and letters or digits: %w", value, ErrInvalidAdminID)
		}
	}
	return id, nil
}

// Admins reads adminlist.txt. A missing file is an empty list.
func (Runner) Admins(inst instance.Instance) ([]game.Admin, error) {
	ids, err := loadAdminIDs(AdminFile(inst))
	if err != nil {
		return nil, err
	}
	list := make([]game.Admin, 0, len(ids))
	for _, id := range ids {
		list = append(list, game.Admin{ID: id, Privileges: "full"})
	}
	return list, nil
}

// AddAdmin appends one Klei id. The server has no limited level and no name.
func (Runner) AddAdmin(inst instance.Instance, id, _ string, limited bool) (bool, error) {
	if limited {
		return false, fmt.Errorf("Don't Starve Together admins are always full: %w", instance.ErrInvalid)
	}
	parsed, err := ParseAdminID(id)
	if err != nil {
		return false, err
	}

	path := AdminFile(inst)
	ids, err := loadAdminIDs(path)
	if err != nil {
		return false, err
	}
	for _, existing := range ids {
		if existing == parsed {
			return false, nil
		}
	}
	ids = append(ids, parsed)
	if err := saveAdminIDs(path, ids); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveAdmin drops one Klei id.
func (Runner) RemoveAdmin(inst instance.Instance, id string) error {
	parsed, err := ParseAdminID(id)
	if err != nil {
		return err
	}

	path := AdminFile(inst)
	ids, err := loadAdminIDs(path)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(ids))
	for _, existing := range ids {
		if existing != parsed {
			kept = append(kept, existing)
		}
	}
	if len(kept) == len(ids) {
		return fmt.Errorf("%s is not an admin of %q", parsed, inst.Name)
	}
	return saveAdminIDs(path, kept)
}

// loadAdminIDs reads one id per line, ignoring blanks and comments.
func loadAdminIDs(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %q: %w", path, err)
	}

	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ids = append(ids, line)
	}
	return ids, nil
}

// saveAdminIDs writes one id per line, the shape the server reads.
func saveAdminIDs(path string, ids []string) error {
	// The cluster directory only exists after the first start, but an admin can
	// be granted before that, so create it here.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cluster directory: %w", err)
	}

	var b strings.Builder
	for _, id := range ids {
		b.WriteString(id)
		b.WriteByte('\n')
	}
	if err := atomicfile.Replace(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("save %q: %w", path, err)
	}
	return nil
}
