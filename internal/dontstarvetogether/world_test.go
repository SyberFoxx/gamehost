package dontstarvetogether

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// seedWorld creates save folders and the cluster files that must survive a reset.
func seedWorld(t *testing.T, inst instance.Instance, withCaves bool) {
	t.Helper()
	files := []string{
		filepath.Join(ClusterDir(inst), "cluster.ini"),
		filepath.Join(ClusterDir(inst), "cluster_token.txt"),
		filepath.Join(ClusterDir(inst), "adminlist.txt"),
		filepath.Join(ShardDir(inst), "server.ini"),
		ServerLogPath(inst),
		filepath.Join(WorldSavePath(inst, Shard), "leveldataoverride.lua"),
	}
	if withCaves {
		files = append(files,
			filepath.Join(CavesDir(inst), "server.ini"),
			filepath.Join(WorldSavePath(inst, CavesShard), "save"),
		)
	}
	for _, path := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte("sentinel"), 0o644); err != nil {
			t.Fatalf("write %q: %v", path, err)
		}
	}
}

func TestResetWorldRemovesBothShards(t *testing.T) {
	inst := testInstance(t)
	inst.Caves = true
	seedWorld(t, inst, true)

	if err := (Runner{}).ResetWorld(inst); err != nil {
		t.Fatalf("ResetWorld: %v", err)
	}
	for _, shard := range []string{Shard, CavesShard} {
		if _, err := os.Stat(WorldSavePath(inst, shard)); !os.IsNotExist(err) {
			t.Errorf("%s save still exists (err = %v)", shard, err)
		}
	}
	for _, path := range []string{
		filepath.Join(ClusterDir(inst), "cluster.ini"),
		filepath.Join(ClusterDir(inst), "cluster_token.txt"),
		filepath.Join(ClusterDir(inst), "adminlist.txt"),
		filepath.Join(ShardDir(inst), "server.ini"),
		ServerLogPath(inst),
		filepath.Join(CavesDir(inst), "server.ini"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("reset removed %q: %v", path, err)
		}
	}
}

func TestResetWorldLeavesUnusedCavesAlone(t *testing.T) {
	inst := testInstance(t)
	seedWorld(t, inst, true)

	if err := (Runner{}).ResetWorld(inst); err != nil {
		t.Fatalf("ResetWorld: %v", err)
	}
	if _, err := os.Stat(WorldSavePath(inst, Shard)); !os.IsNotExist(err) {
		t.Errorf("Master save still exists (err = %v)", err)
	}
	if _, err := os.Stat(WorldSavePath(inst, CavesShard)); err != nil {
		t.Errorf("reset removed a Caves save it does not own: %v", err)
	}
}

func TestResetWorldIsIdempotent(t *testing.T) {
	inst := testInstance(t)
	if err := (Runner{}).ResetWorld(inst); err != nil {
		t.Fatalf("first ResetWorld: %v", err)
	}
	if err := (Runner{}).ResetWorld(inst); err != nil {
		t.Fatalf("second ResetWorld: %v", err)
	}
}
