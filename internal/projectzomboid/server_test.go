package projectzomboid

import (
	"path/filepath"
	"strings"
	"testing"

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
