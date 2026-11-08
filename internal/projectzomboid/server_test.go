package projectzomboid

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// newInstance builds a Project Zomboid instance for the driver tests.
func newInstance(t *testing.T) instance.Instance {
	t.Helper()

	inst, err := instance.New("zomboid", instance.Options{
		Game:          instance.GameProjectZomboid,
		WorldName:     "base",
		Port:          16261,
		UDPPort:       16262,
		AdminPassword: "secret",
		InstallDir:    "/srv/files",
		DataDir:       "/srv/data",
	})
	if err != nil {
		t.Fatalf("instance.New() error = %v", err)
	}
	return inst
}

// covers the launch arguments
func TestArgs(t *testing.T) {
	t.Parallel()

	inst := newInstance(t)
	got := strings.Join(Args(inst), " ")

	for _, want := range []string{
		"-servername base",
		"-port 16261",
		"-udpport 16262",
		"-cachedir=/srv/data",
		"-adminpassword secret",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Args() = %q, missing %q", got, want)
		}
	}
}

// covers the launch arguments when the admin password is already configured
func TestArgsWithoutAdminPassword(t *testing.T) {
	t.Parallel()

	inst := newInstance(t)
	inst.AdminPassword = ""

	if got := strings.Join(Args(inst), " "); strings.Contains(got, "-adminpassword") {
		t.Errorf("Args() = %q, want no admin password flag", got)
	}
}

// covers the SteamCMD installation arguments
func TestInstallArgs(t *testing.T) {
	t.Parallel()

	got := strings.Join(InstallArgs("/srv/files"), " ")
	for _, want := range []string{"+force_install_dir /srv/files", "+app_update 380870 validate"} {
		if !strings.Contains(got, want) {
			t.Errorf("InstallArgs() = %q, missing %q", got, want)
		}
	}
}

// covers the launch script and the runner adapter
func TestRunner(t *testing.T) {
	t.Parallel()

	inst := newInstance(t)
	runner := Runner{}

	launch, err := runner.Launch(inst)
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if want := filepath.Join("/srv/files", Script); launch.Path != want {
		t.Errorf("Launch().Path = %q, want %q", launch.Path, want)
	}
	if launch.Dir != "/srv/files" {
		t.Errorf("Launch().Dir = %q, want the install dir", launch.Dir)
	}
	if label, value := runner.Join(inst); label != "join" || value != "port 16261 UDP" {
		t.Errorf("Join() = %q, %q; want the game port", label, value)
	}
	if id := runner.SessionID(inst); id != "" {
		t.Errorf("SessionID() = %q, want empty", id)
	}
}

// covers the install script lookup
func TestServerPath(t *testing.T) {
	t.Parallel()

	if got, want := (Installer{}).ServerPath("/srv/files"), filepath.Join("/srv/files", Script); got != want {
		t.Errorf("ServerPath() = %q, want %q", got, want)
	}
	if got := (Installer{}).AppID(); got != AppID {
		t.Errorf("AppID() = %q, want %q", got, AppID)
	}
}

// launcherJSON is the file the server reads its JVM arguments from.
const launcherJSON = `{
	"mainClass": "zombie/network/GameServer",
	"classpath": [
		"java/.",
		"java/projectzomboid.jar"
	],
	"vmArgs": [
		"-Djava.awt.headless=true",
		"-Xmx8g",
		"-Dzomboid.steam=1",
		"-XX:+UseZGC"
	],
	"futureKey": {
		"nested": "kept"
	}
}
`

// writeLauncher writes a launcher json for a temporary install directory.
func writeLauncher(t *testing.T, body string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, ConfigFile)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// readLauncher decodes the heap and the rest of the file.
func readLauncher(t *testing.T, dir string) ([]string, map[string]json.RawMessage, []byte) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the rewritten launcher is not valid json: %v\n%s", err, raw)
	}
	var args []string
	if err := json.Unmarshal(doc["vmArgs"], &args); err != nil {
		t.Fatalf("vmArgs = %s, want a string array: %v", doc["vmArgs"], err)
	}
	return args, doc, raw
}

// covers the heap rewrite before a launch
func TestPrepareWritesTheHeap(t *testing.T) {
	dir := writeLauncher(t, launcherJSON)
	inst := newInstance(t)
	inst.InstallDir = dir
	inst.Memory = "4g"

	if err := prepare(inst, 16<<30); err != nil {
		t.Fatalf("prepare error = %v", err)
	}

	args, doc, _ := readLauncher(t, dir)
	want := []string{
		"-Djava.awt.headless=true",
		"-Xmx4g",
		"-Dzomboid.steam=1",
		"-XX:+UseZGC",
	}
	if !slices.Equal(args, want) {
		t.Errorf("vmArgs = %v, want %v", args, want)
	}
	// Every other key survives, including one this build does not know.
	if _, ok := doc["futureKey"]; !ok {
		t.Errorf("the rewrite dropped futureKey")
	}
	if got := string(doc["mainClass"]); got != `"zombie/network/GameServer"` {
		t.Errorf("mainClass = %s", got)
	}

	// A second prepare with the same value must not touch the file.
	before, err := os.Stat(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := prepare(inst, 16<<30); err != nil {
		t.Fatalf("second prepare error = %v", err)
	}
	after, err := os.Stat(filepath.Join(dir, ConfigFile))
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("an unchanged heap rewrote the file")
	}
}

// covers the default heap and a launcher without one
func TestPrepareFillsInTheHeap(t *testing.T) {
	// An instance without a value keeps the default the server ships with.
	dir := writeLauncher(t, launcherJSON)
	inst := newInstance(t)
	inst.InstallDir = dir
	if err := prepare(inst, 16<<30); err != nil {
		t.Fatalf("prepare error = %v", err)
	}
	args, _, _ := readLauncher(t, dir)
	if want := "-Xmx" + instance.DefaultProjectZomboidMemory; !slices.Contains(args, want) {
		t.Errorf("vmArgs = %v, want %q", args, want)
	}

	// A file without a heap gets one; the other arguments keep their order.
	body := `{"vmArgs": ["-Dzomboid.steam=1", "-XX:+UseZGC"]}`
	dir = writeLauncher(t, body)
	inst.InstallDir = dir
	inst.Memory = "2g"
	if err := prepare(inst, 16<<30); err != nil {
		t.Fatalf("prepare error = %v", err)
	}
	args, _, _ = readLauncher(t, dir)
	want := []string{"-Dzomboid.steam=1", "-XX:+UseZGC", "-Xmx2g"}
	if !slices.Equal(args, want) {
		t.Errorf("vmArgs = %v, want %v", args, want)
	}
}

// covers refusing a heap this machine cannot hold and a broken file
func TestPrepareRefuses(t *testing.T) {
	dir := writeLauncher(t, launcherJSON)
	inst := newInstance(t)
	inst.InstallDir = dir
	inst.Memory = "64g"

	err := prepare(inst, 16<<30)
	if !errors.Is(err, instance.ErrInvalid) {
		t.Fatalf("prepare error = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "64g") || !strings.Contains(err.Error(), "16g") {
		t.Errorf("error = %v, want the requested and the available size", err)
	}
	// The file is untouched when the launch is refused.
	args, _, _ := readLauncher(t, dir)
	if !slices.Contains(args, "-Xmx8g") {
		t.Errorf("vmArgs = %v, the refused heap was written anyway", args)
	}

	if err := prepare(inst, 0); err != nil {
		t.Fatalf("prepare without a known total error = %v", err)
	}

	// A missing or malformed launcher is reported, never created.
	missing := instance.Instance{InstallDir: t.TempDir(), Memory: "4g"}
	if err := prepare(missing, 16<<30); err == nil || !strings.Contains(err.Error(), ConfigFile) {
		t.Errorf("prepare without a launcher error = %v, want the file name", err)
	}
	broken := writeLauncher(t, "{not json")
	inst.InstallDir = broken
	inst.Memory = "4g"
	if err := prepare(inst, 16<<30); err == nil || !strings.Contains(err.Error(), ConfigFile) {
		t.Errorf("prepare with a broken launcher error = %v, want the file name", err)
	}
}
