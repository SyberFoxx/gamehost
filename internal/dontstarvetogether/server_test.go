package dontstarvetogether

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// testInstance returns an online cluster rooted in a temporary directory.
func testInstance(t *testing.T) instance.Instance {
	t.Helper()
	dir := t.TempDir()
	inst, err := instance.New("cave", instance.Options{
		Game:         instance.GameDontStarveTogether,
		InstallDir:   filepath.Join(dir, "files"),
		DataDir:      filepath.Join(dir, "data"),
		WorldName:    "Night Owls",
		MaxPlayers:   4,
		Port:         11000,
		ClusterToken: "pds-g^KU_test",
	})
	if err != nil {
		t.Fatalf("new instance: %v", err)
	}
	return inst
}

func TestArgs(t *testing.T) {
	tests := []struct {
		name    string
		offline bool
		want    string
	}{
		{
			name: "online",
			want: "-persistent_storage_root %s -conf_dir gamehost -cluster cave -shard Master -players 4 -port 11000",
		},
		{
			name:    "offline adds the flag",
			offline: true,
			want:    "-persistent_storage_root %s -conf_dir gamehost -cluster cave -shard Master -players 4 -port 11000 -offline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := testInstance(t)
			if tt.offline {
				inst.Network.Mode = instance.NetworkOffline
			}
			want := strings.Replace(tt.want, "%s", inst.DataDir, 1)
			if got := strings.Join(Args(inst), " "); got != want {
				t.Fatalf("Args() =\n%q\nwant\n%q", got, want)
			}
		})
	}
}

func TestPrepareWritesCluster(t *testing.T) {
	inst := testInstance(t)
	if err := Prepare(inst); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	clusterDir := ClusterDir(inst)
	for _, path := range []string{
		filepath.Join(clusterDir, "cluster.ini"),
		filepath.Join(clusterDir, TokenFile),
		filepath.Join(ShardDir(inst), "server.ini"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}

	cluster, err := os.ReadFile(filepath.Join(clusterDir, "cluster.ini"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"cluster_name = Night Owls",
		"max_players = 4",
		"offline_cluster = false",
	} {
		if !strings.Contains(string(cluster), want) {
			t.Errorf("cluster.ini lacks %q:\n%s", want, cluster)
		}
	}

	shard, err := os.ReadFile(filepath.Join(ShardDir(inst), "server.ini"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"server_port = 11000", "is_master = true"} {
		if !strings.Contains(string(shard), want) {
			t.Errorf("server.ini lacks %q:\n%s", want, shard)
		}
	}

	info, err := os.Stat(filepath.Join(clusterDir, TokenFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token perm = %o, want 600", perm)
	}
	token, err := os.ReadFile(filepath.Join(clusterDir, TokenFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(token)) != inst.ClusterToken {
		t.Errorf("token = %q, want %q", token, inst.ClusterToken)
	}
}

func TestPrepareKeepsPlayerEdits(t *testing.T) {
	inst := testInstance(t)
	if err := Prepare(inst); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	// The server rewrites these files, so a second prepare must not clobber them.
	clusterPath := filepath.Join(ClusterDir(inst), "cluster.ini")
	edited := []byte("[NETWORK]\ncluster_name = Edited\n")
	if err := os.WriteFile(clusterPath, edited, 0o644); err != nil {
		t.Fatal(err)
	}

	inst.ClusterToken = "pds-g^KU_rotated"
	if err := Prepare(inst); err != nil {
		t.Fatalf("second prepare: %v", err)
	}

	cluster, err := os.ReadFile(clusterPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(cluster) != string(edited) {
		t.Errorf("prepare overwrote edited cluster.ini:\n%s", cluster)
	}
	token, err := os.ReadFile(filepath.Join(ClusterDir(inst), TokenFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(token)) != "pds-g^KU_rotated" {
		t.Errorf("token was not refreshed: %q", token)
	}
}

func TestPrepareOfflineWritesNoToken(t *testing.T) {
	inst := testInstance(t)
	inst.Network.Mode = instance.NetworkOffline
	inst.ClusterToken = ""
	if err := Prepare(inst); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	if _, err := os.Stat(filepath.Join(ClusterDir(inst), TokenFile)); !os.IsNotExist(err) {
		t.Fatalf("offline cluster wrote a token file: %v", err)
	}
	cluster, err := os.ReadFile(filepath.Join(ClusterDir(inst), "cluster.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cluster), "offline_cluster = true") {
		t.Errorf("cluster.ini lacks the offline switch:\n%s", cluster)
	}
}

func TestPrepareRejectsOtherGame(t *testing.T) {
	inst := testInstance(t)
	inst.Game = instance.GameCoreKeeper
	if err := Prepare(inst); err == nil {
		t.Fatal("Prepare accepted a Core Keeper instance")
	}
}

func TestLaunchRunsFromBin64(t *testing.T) {
	inst := testInstance(t)
	launch, err := (Runner{}).Launch(inst)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if want := ServerPath(inst.InstallDir); launch.Path != want {
		t.Errorf("path = %q, want %q", launch.Path, want)
	}
	if want := filepath.Join(inst.InstallDir, "bin64"); launch.Dir != want {
		t.Errorf("dir = %q, want %q", launch.Dir, want)
	}
}

func TestInstallArgs(t *testing.T) {
	args := strings.Join(InstallArgs("/games/dst"), " ")
	want := "+force_install_dir /games/dst +login anonymous +app_update 343050 validate +quit"
	if args != want {
		t.Errorf("InstallArgs() = %q, want %q", args, want)
	}
}
