package admins

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers the accepted and rejected Steam id shapes
func TestParseSteamID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    uint64
		wantErr error
	}{
		{"individual account", "76561198012345678", 76561198012345678, nil},
		{"lower bound", "76561197960265728", 76561197960265728, nil},
		{"upper bound", "76561202255233023", 76561202255233023, nil},
		{"surrounding spaces", "  76561198012345678 ", 76561198012345678, nil},
		{"too short", "7656119801234567", 0, ErrInvalidSteamID},
		{"too long", "765611980123456789", 0, ErrInvalidSteamID},
		{"not a number", "7656119801234567x", 0, ErrInvalidSteamID},
		{"below the account range", "76561197960265727", 0, ErrInvalidSteamID},
		{"above the account range", "76561202255233024", 0, ErrInvalidSteamID},
		{"empty", "", 0, ErrInvalidSteamID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseSteamID(tt.value)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ParseSteamID(%q) error = %v, want %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("ParseSteamID(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

// covers a missing file reporting an empty list
func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	file, err := Load(filepath.Join(t.TempDir(), "Admins.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(file.AdminList) != 0 {
		t.Errorf("AdminList = %v, want none", file.AdminList)
	}
}

// covers adding, updating and removing entries
func TestAddAndRemove(t *testing.T) {
	t.Parallel()

	file := &File{}

	added, err := file.Add(76561198012345678, "Ramsés", Full)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !added {
		t.Error("Add() = false, want a new entry")
	}
	if _, err := file.Add(76561198012345679, "Amigo", Limited); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Adding a known id updates it instead of duplicating it.
	added, err = file.Add(76561198012345678, "Ramsés", Limited)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if added {
		t.Error("Add() = true for a known id, want an update")
	}
	if len(file.AdminList) != 2 {
		t.Fatalf("AdminList = %v, want two entries", file.AdminList)
	}
	if got := file.AdminList[0]; got.Name != "Ramsés" || got.Privileges != Limited {
		t.Errorf("entry = %+v, want the updated name and privileges", got)
	}

	if file.Remove(76561198012345678) != true {
		t.Error("Remove() = false, want true")
	}
	if file.Remove(76561198012345678) != false {
		t.Error("Remove() = true for a missing id")
	}
	if len(file.AdminList) != 1 {
		t.Errorf("AdminList = %v, want one entry", file.AdminList)
	}

	if _, err := file.Add(76561198012345678, "Ramsés", 7); err == nil {
		t.Error("Add() error = nil for an unknown privilege level")
	}
}

// covers writing the server's own format and reading it back
func TestSaveAndLoad(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Admins.json")
	file := &File{}
	if _, err := file.Add(76561198012345678, "Ramsés", Full); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if _, err := file.Add(76561198012345679, "Amigo", Limited); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := file.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"adminList"`, `"index": 1`, `"privileges": 2`, `"steamId": 76561198012345678`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("file missing %q:\n%s", want, data)
		}
	}
	if !strings.Contains(string(data), "    \"adminList\"") {
		t.Errorf("file is not indented with four spaces:\n%s", data)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.AdminList) != 2 {
		t.Fatalf("AdminList = %v, want two entries", loaded.AdminList)
	}
	if loaded.AdminList[1].Index != 2 || loaded.AdminList[1].SteamID != 76561198012345679 {
		t.Errorf("second entry = %+v, want index 2 for the second steam id", loaded.AdminList[1])
	}
}

// covers an empty list being written as an array, not null
func TestSaveEmpty(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Admins.json")
	if err := (&File{}).Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"adminList": []`) {
		t.Errorf("file = %s, want an empty array", data)
	}
}

// covers keeping fields this package does not know about
func TestSaveKeepsUnknownFields(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "Admins.json")
	original := `{
    "adminList": [
        {
            "index": 1,
            "privileges": 2,
            "name": "Tsukii",
            "steamId": 76561198103484834,
            "crossPlatformId": 16777343
        }
    ]
}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(file.AdminList) != 1 || file.AdminList[0].Name != "Tsukii" {
		t.Fatalf("AdminList = %+v, want the existing admin", file.AdminList)
	}
	if _, err := file.Add(76561198012345678, "Amigo", Limited); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := file.Save(path); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"crossPlatformId": 16777343`) {
		t.Errorf("save dropped an unknown field:\n%s", data)
	}
	if !strings.Contains(string(data), `"name": "Amigo"`) {
		t.Errorf("save dropped the new admin:\n%s", data)
	}
}
