package dontstarvetogether

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/syberfoxx/gamehost/internal/instance"
)

// WorldSavePath returns the generated world data of one shard. The server
// creates this folder on first start and reads it on every later one.
func WorldSavePath(inst instance.Instance, shard string) string {
	return filepath.Join(ClusterDir(inst), shard, "save")
}

// ResetWorld removes the generated save folders so the next start creates new
// worlds from the existing leveldataoverride.lua. The cluster configuration,
// token, admin list, logs and backups are not world data and stay in place.
func (Runner) ResetWorld(inst instance.Instance) error {
	shards := []string{Shard}
	if inst.Caves {
		shards = append(shards, CavesShard)
	}
	for _, shard := range shards {
		path := WorldSavePath(inst, shard)
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove world data %q: %w", path, err)
		}
	}
	return nil
}
