package dontstarvetogether

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
)

// seedMods creates the directories and optional files a mod write needs.
func seedMods(t *testing.T, inst instance.Instance, setup, master, caves string) {
	t.Helper()
	dirs := []string{filepath.Dir(ModSetupPath(inst)), ShardDir(inst)}
	if inst.Caves {
		dirs = append(dirs, CavesDir(inst))
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create %q: %v", dir, err)
		}
	}
	files := []struct {
		path string
		data string
	}{
		{ModSetupPath(inst), setup},
		{ModOverridePath(inst, Shard), master},
	}
	if inst.Caves {
		files = append(files, struct {
			path string
			data string
		}{ModOverridePath(inst, CavesShard), caves})
	}
	for _, file := range files {
		if file.data == "" {
			continue
		}
		if err := os.WriteFile(file.path, []byte(file.data), 0o644); err != nil {
			t.Fatalf("write %q: %v", file.path, err)
		}
	}
}

// readModFile returns the contents of a mod file, failing when it is absent.
func readModFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return string(data)
}

func TestModsRoundTrip(t *testing.T) {
	inst := testInstance(t)
	seedMods(t, inst, "", "", "")

	mods, err := (Runner{}).Mods(inst)
	if err != nil {
		t.Fatalf("list empty mods: %v", err)
	}
	if len(mods.Workshop) != 0 || len(mods.Enabled) != 0 {
		t.Fatalf("empty mods = %+v", mods)
	}

	if err := (Runner{}).SaveMods(inst, game.Mods{Workshop: []string{"123", "456", "123"}}); err != nil {
		t.Fatalf("save mods: %v", err)
	}
	setup := readModFile(t, ModSetupPath(inst))
	for _, want := range []string{`ServerModSetup("123")`, `ServerModSetup("456")`} {
		if !strings.Contains(setup, want) {
			t.Errorf("setup lacks %s:\n%s", want, setup)
		}
	}
	if got := strings.Count(setup, "ServerModSetup("); got != 2 {
		t.Errorf("setup has %d setup lines, want 2:\n%s", got, setup)
	}

	override := readModFile(t, ModOverridePath(inst, Shard))
	for _, want := range []string{`["workshop-123"] = { enabled = true }`, `["workshop-456"] = { enabled = true }`} {
		if !strings.Contains(override, want) {
			t.Errorf("modoverrides lacks %s:\n%s", want, override)
		}
	}

	mods, err = (Runner{}).Mods(inst)
	if err != nil {
		t.Fatalf("list saved mods: %v", err)
	}
	if got := strings.Join(mods.Workshop, ","); got != "123,456" {
		t.Errorf("workshop = %q, want 123,456", got)
	}
	if got := strings.Join(mods.Enabled, ","); got != "workshop-123,workshop-456" {
		t.Errorf("enabled = %q, want workshop-123,workshop-456", got)
	}
}

func TestModsWritesBothShards(t *testing.T) {
	inst := testInstance(t)
	inst.Caves = true
	seedMods(t, inst, "", "", "")

	if err := (Runner{}).SaveMods(inst, game.Mods{Workshop: []string{"789"}}); err != nil {
		t.Fatalf("save mods: %v", err)
	}
	for _, shard := range []string{Shard, CavesShard} {
		override := readModFile(t, ModOverridePath(inst, shard))
		if !strings.Contains(override, `["workshop-789"] = { enabled = true }`) {
			t.Errorf("%s modoverrides lacks the mod:\n%s", shard, override)
		}
	}
}

func TestModsPreservesPlayerFiles(t *testing.T) {
	inst := testInstance(t)
	const setup = "-- kept by hand\nServerModCollectionSetup(\"999\")\nServerModSetup(\"111\")\n"
	const override = "return {\n" +
		"  [\"workshop-111\"] = { enabled = true },\n" +
		"  [\"workshop-222\"] = {\n" +
		"    enabled = true,\n" +
		"    configuration_options = {\n" +
		"      foo = \"bar\",\n" +
		"    },\n" +
		"  },\n" +
		"}\n"
	seedMods(t, inst, setup, override, "")

	mods := game.Mods{Workshop: []string{"333"}, Enabled: []string{"workshop-222"}}
	if err := (Runner{}).SaveMods(inst, mods); err != nil {
		t.Fatalf("save mods: %v", err)
	}

	gotSetup := readModFile(t, ModSetupPath(inst))
	for _, want := range []string{"-- kept by hand", `ServerModCollectionSetup("999")`, `ServerModSetup("333")`} {
		if !strings.Contains(gotSetup, want) {
			t.Errorf("setup lacks %q:\n%s", want, gotSetup)
		}
	}
	if strings.Contains(gotSetup, `ServerModSetup("111")`) {
		t.Errorf("setup kept the removed mod:\n%s", gotSetup)
	}

	gotOverride := readModFile(t, ModOverridePath(inst, Shard))
	for _, want := range []string{`["workshop-222"] = {`, `foo = "bar"`, `["workshop-333"] = { enabled = true }`} {
		if !strings.Contains(gotOverride, want) {
			t.Errorf("modoverrides lacks %q:\n%s", want, gotOverride)
		}
	}
	if strings.Contains(gotOverride, `["workshop-111"]`) {
		t.Errorf("modoverrides kept the removed mod:\n%s", gotOverride)
	}
}

func TestModsNumericModInstallsWorkshop(t *testing.T) {
	inst := testInstance(t)
	seedMods(t, inst, "", "", "")

	if err := (Runner{}).SaveMods(inst, game.Mods{Enabled: []string{"12345"}}); err != nil {
		t.Fatalf("save mods: %v", err)
	}
	setup := readModFile(t, ModSetupPath(inst))
	if !strings.Contains(setup, `ServerModSetup("12345")`) {
		t.Errorf("setup lacks the workshop install:\n%s", setup)
	}
	override := readModFile(t, ModOverridePath(inst, Shard))
	if !strings.Contains(override, `["workshop-12345"] = { enabled = true }`) {
		t.Errorf("modoverrides lacks the mod:\n%s", override)
	}
}

func TestModsRejectsInvalidID(t *testing.T) {
	inst := testInstance(t)
	const setup = "-- sentinel\n"
	seedMods(t, inst, setup, "", "")

	err := (Runner{}).SaveMods(inst, game.Mods{Workshop: []string{"12abc"}})
	if !errors.Is(err, ErrInvalidMod) {
		t.Fatalf("SaveMods error = %v, want ErrInvalidMod", err)
	}
	if got := readModFile(t, ModSetupPath(inst)); got != setup {
		t.Errorf("setup changed after a rejected save: %q", got)
	}
}
