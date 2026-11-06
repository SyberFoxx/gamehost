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

func TestCavesShard(t *testing.T) {
	inst := testInstance(t)
	inst.Caves = true

	t.Run("launch adds a second shard", func(t *testing.T) {
		launch, err := (Runner{}).Launch(inst)
		if err != nil {
			t.Fatalf("launch: %v", err)
		}
		if len(launch.Extra) != 1 {
			t.Fatalf("extra launches = %d, want 1", len(launch.Extra))
		}
		args := strings.Join(launch.Extra[0].Args, " ")
		for _, want := range []string{"-shard Caves", "-port 11001"} {
			if !strings.Contains(args, want) {
				t.Errorf("caves args %q lack %q", args, want)
			}
		}
	})

	t.Run("prepare writes both shards", func(t *testing.T) {
		if err := Prepare(inst); err != nil {
			t.Fatalf("prepare: %v", err)
		}

		cluster, err := os.ReadFile(filepath.Join(ClusterDir(inst), "cluster.ini"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"[SHARD]", "shard_enabled = true", "master_port = 10888", "cluster_key = "} {
			if !strings.Contains(string(cluster), want) {
				t.Errorf("cluster.ini lacks %q:\n%s", want, cluster)
			}
		}

		caves, err := os.ReadFile(filepath.Join(CavesDir(inst), "server.ini"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"server_port = 11001", "is_master = false", "name = Caves"} {
			if !strings.Contains(string(caves), want) {
				t.Errorf("Caves/server.ini lacks %q:\n%s", want, caves)
			}
		}
	})
}

func TestCavesSectionIsNotDuplicated(t *testing.T) {
	inst := testInstance(t)
	inst.Caves = true
	if err := Prepare(inst); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// The section already exists, so a second prepare must not add a key again.
	if err := Prepare(inst); err != nil {
		t.Fatalf("second prepare: %v", err)
	}

	cluster, err := os.ReadFile(filepath.Join(ClusterDir(inst), "cluster.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(cluster), "[SHARD]"); got != 1 {
		t.Errorf("cluster.ini has %d shard sections:\n%s", got, cluster)
	}
}

func TestAdminList(t *testing.T) {
	inst := testInstance(t)

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid", "KU_QDNly1sC", false},
		{"empty", "", true},
		{"no prefix", "qdnly1sc", true},
		{"too short", "KU_ab", true},
		{"dash", "KU_ab-cd", true},
		{"spaces", "KU_ab cd", true},
	}
	for _, tt := range tests {
		t.Run("parse "+tt.name, func(t *testing.T) {
			_, err := ParseAdminID(tt.id)
			if gotErr := err != nil; gotErr != tt.wantErr {
				t.Fatalf("ParseAdminID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}

	t.Run("add, list and remove", func(t *testing.T) {
		added, err := (Runner{}).AddAdmin(inst, " KU_QDNly1sC ", "", false)
		if err != nil || !added {
			t.Fatalf("AddAdmin() = %v, %v", added, err)
		}
		if again, err := (Runner{}).AddAdmin(inst, "KU_QDNly1sC", "", false); err != nil || again {
			t.Fatalf("duplicate AddAdmin() = %v, %v", again, err)
		}

		list, err := (Runner{}).Admins(inst)
		if err != nil {
			t.Fatalf("Admins(): %v", err)
		}
		if len(list) != 1 || list[0].ID != "KU_QDNly1sC" {
			t.Fatalf("Admins() = %#v", list)
		}

		if err := (Runner{}).RemoveAdmin(inst, "KU_unknown1"); err == nil {
			t.Fatal("RemoveAdmin accepted a missing id")
		}
		if err := (Runner{}).RemoveAdmin(inst, "KU_QDNly1sC"); err != nil {
			t.Fatalf("RemoveAdmin(): %v", err)
		}
		data, err := os.ReadFile(AdminFile(inst))
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(data)) != "" {
			t.Errorf("adminlist.txt = %q after the removal", data)
		}
	})

	t.Run("limited is refused", func(t *testing.T) {
		if _, err := (Runner{}).AddAdmin(inst, "KU_QDNly1sC", "", true); err == nil {
			t.Fatal("AddAdmin accepted limited privileges")
		}
	})
}

func TestInstallArgs(t *testing.T) {
	args := strings.Join(InstallArgs("/games/dst"), " ")
	want := "+force_install_dir /games/dst +login anonymous +app_update 343050 validate +quit"
	if args != want {
		t.Errorf("InstallArgs() = %q, want %q", args, want)
	}
}
