// Package admins reads and writes the Admins.json file of a Core Keeper server.
package admins

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/syberfoxx/gamehost/internal/atomicfile"
)

// Privilege levels Core Keeper understands.
const (
	Limited = 1
	Full    = 2
)

// Individual Steam accounts start at this id and span 2^32 values.
const (
	steamIDBase = 76561197960265728
	steamIDMax  = steamIDBase + (1 << 32) - 1
)

// ErrInvalidSteamID reports a value that is not an individual Steam account.
var ErrInvalidSteamID = errors.New("invalid steam id")

// Entry is one admin as Core Keeper stores it.
// Fields this package does not know, such as crossPlatformId, are kept in extra
// so a save never drops what the server wrote.
type Entry struct {
	Index      int
	Privileges int
	Name       string
	SteamID    uint64

	extra map[string]json.RawMessage
}

// File mirrors the Admins.json document.
type File struct {
	AdminList []Entry `json:"adminList"`
}

// UnmarshalJSON reads the known fields and keeps every other one untouched.
func (e *Entry) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	for key, target := range map[string]any{
		"index":      &e.Index,
		"privileges": &e.Privileges,
		"name":       &e.Name,
		"steamId":    &e.SteamID,
	} {
		value, ok := raw[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(value, target); err != nil {
			return fmt.Errorf("decode %q: %w", key, err)
		}
		delete(raw, key)
	}

	e.extra = raw
	return nil
}

// MarshalJSON writes the known fields back together with the unknown ones.
func (e Entry) MarshalJSON() ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(e.extra)+4)
	for key, value := range e.extra {
		fields[key] = value
	}

	for key, value := range map[string]any{
		"index":      e.Index,
		"privileges": e.Privileges,
		"name":       e.Name,
		"steamId":    e.SteamID,
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode %q: %w", key, err)
		}
		fields[key] = encoded
	}
	return json.Marshal(fields)
}

// Load reads Admins.json. A missing file is an empty list, which is what a
// fresh server starts with.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &File{}, nil
		}
		return nil, fmt.Errorf("read %q: %w", path, err)
	}

	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode %q: %w", path, err)
	}
	return &file, nil
}

// Save writes Admins.json atomically, in the shape the server itself writes.
func (f *File) Save(path string) error {
	if f.AdminList == nil {
		f.AdminList = []Entry{}
	}
	f.reindex()

	data, err := json.MarshalIndent(f, "", "    ")
	if err != nil {
		return fmt.Errorf("encode %q: %w", path, err)
	}
	data = append(data, '\n')

	if err := atomicfile.Replace(path, data, 0o644); err != nil {
		return fmt.Errorf("save %q: %w", path, err)
	}
	return nil
}

// Add inserts or updates one admin and returns whether it was new.
func (f *File) Add(steamID uint64, name string, privileges int) (bool, error) {
	if privileges != Full && privileges != Limited {
		return false, fmt.Errorf("privileges %d must be %d (limited) or %d (full)", privileges, Limited, Full)
	}

	for i := range f.AdminList {
		if f.AdminList[i].SteamID == steamID {
			f.AdminList[i].Name = name
			f.AdminList[i].Privileges = privileges
			return false, nil
		}
	}

	f.AdminList = append(f.AdminList, Entry{
		Privileges: privileges,
		Name:       name,
		SteamID:    steamID,
	})
	return true, nil
}

// Remove deletes one admin and reports whether it was there.
func (f *File) Remove(steamID uint64) bool {
	for i, entry := range f.AdminList {
		if entry.SteamID == steamID {
			f.AdminList = append(f.AdminList[:i], f.AdminList[i+1:]...)
			return true
		}
	}
	return false
}

// reindex keeps the one-based order the server expects.
func (f *File) reindex() {
	for i := range f.AdminList {
		f.AdminList[i].Index = i + 1
	}
}

// ParseSteamID validates an individual Steam account id.
func ParseSteamID(value string) (uint64, error) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) != 17 {
		return 0, fmt.Errorf("steam id %q must have 17 digits: %w", value, ErrInvalidSteamID)
	}
	id, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("steam id %q: %w", value, ErrInvalidSteamID)
	}
	if id < steamIDBase || id > steamIDMax {
		return 0, fmt.Errorf("steam id %q is not an individual account: %w", value, ErrInvalidSteamID)
	}
	return id, nil
}

// PrivilegeLabel names one privilege level for the terminal.
func PrivilegeLabel(privileges int) string {
	switch privileges {
	case Full:
		return "full"
	case Limited:
		return "limited"
	default:
		return fmt.Sprintf("unknown (%d)", privileges)
	}
}
