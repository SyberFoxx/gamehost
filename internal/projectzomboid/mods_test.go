package projectzomboid

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
)

// modInstance creates a Project Zomboid instance whose ini already exists.
func modInstance(t *testing.T, ini string) instance.Instance {
	t.Helper()
	dir := t.TempDir()
	inst, err := instance.New("zomboid", instance.Options{
		Game:          instance.GameProjectZomboid,
		AdminPassword: "secret",
		WorldName:     "base",
		InstallDir:    filepath.Join(dir, "files"),
		DataDir:       filepath.Join(dir, "data"),
	})
	if err != nil {
		t.Fatalf("new instance: %v", err)
	}
	path := IniPath(inst)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	return inst
}

func TestModsReadWrite(t *testing.T) {
	ini := "PVP=true\nWorkshopItems=123;456\nMods=Hydrocraft;MoreTraits\nMaxPlayers=16\n"
	inst := modInstance(t, ini)
	runner := Runner{}

	mods, err := runner.Mods(inst)
	if err != nil {
		t.Fatalf("Mods(): %v", err)
	}
	if strings.Join(mods.Workshop, ",") != "123,456" || strings.Join(mods.Enabled, ",") != "Hydrocraft,MoreTraits" {
		t.Fatalf("Mods() = %#v", mods)
	}

	mods.Enabled = append(mods.Enabled, "Extra")
	if err := runner.SaveMods(inst, mods); err != nil {
		t.Fatalf("SaveMods(): %v", err)
	}

	data, err := os.ReadFile(IniPath(inst))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"PVP=true",
		"WorkshopItems=123;456",
		"Mods=Hydrocraft;MoreTraits;Extra",
		"MaxPlayers=16",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ini lacks %q:\n%s", want, got)
		}
	}
}

func TestModsAppendMissingKeys(t *testing.T) {
	inst := modInstance(t, "PVP=false\n")
	runner := Runner{}

	if err := runner.SaveMods(inst, game.Mods{Workshop: []string{"789"}, Enabled: []string{"SomeMod"}}); err != nil {
		t.Fatalf("SaveMods(): %v", err)
	}

	data, err := os.ReadFile(IniPath(inst))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"PVP=false", "WorkshopItems=789", "Mods=SomeMod"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("ini lacks %q:\n%s", want, data)
		}
	}
}

func TestModsMissingIni(t *testing.T) {
	inst := modInstance(t, "")
	if err := os.Remove(IniPath(inst)); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{}).Mods(inst); err == nil {
		t.Fatal("Mods() accepted a missing ini")
	}
	if err := (Runner{}).SaveMods(inst, game.Mods{}); err == nil {
		t.Fatal("SaveMods() accepted a missing ini")
	}
}

func TestModValidation(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		parse   func(string) (string, error)
		wantErr bool
	}{
		{"workshop id", "1234567890", ParseWorkshopID, false},
		{"workshop with letters", "12ab", ParseWorkshopID, true},
		{"workshop empty", "", ParseWorkshopID, true},
		{"mod id", "Hydrocraft", ParseModID, false},
		{"mod with dot", "mod.core", ParseModID, false},
		{"mod with space", "my mod", ParseModID, true},
		{"mod with slash", "my/mod", ParseModID, true},
		{"mod empty", "", ParseModID, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.parse(tt.id)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("parse(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestModsDeduplicate(t *testing.T) {
	inst := modInstance(t, "Mods=A;A;B\n")
	runner := Runner{}

	mods, err := runner.Mods(inst)
	if err != nil {
		t.Fatalf("Mods(): %v", err)
	}
	if err := runner.SaveMods(inst, mods); err != nil {
		t.Fatalf("SaveMods(): %v", err)
	}
	data, err := os.ReadFile(IniPath(inst))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Mods=A;B") {
		t.Errorf("duplicates survived:\n%s", data)
	}
}
