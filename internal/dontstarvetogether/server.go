// Package dontstarvetogether adapts the Don't Starve Together dedicated server.
package dontstarvetogether

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/syberfoxx/gamehost/internal/atomicfile"
	"github.com/syberfoxx/gamehost/internal/game"
	"github.com/syberfoxx/gamehost/internal/instance"
)

const (
	// AppID is the Steam application of the dedicated server.
	AppID = "343050"
	// Binary is the 64-bit null renderer, relative to the install directory.
	Binary = "bin64/dontstarve_dedicated_server_nullrenderer_x64"
	// ConfDir is the -conf_dir the driver passes; the cluster lives under it.
	ConfDir = "gamehost"
	// Shard is the surface shard, the one that owns the cluster.
	Shard = "Master"
	// CavesShard is the underground shard, optional and started next to the Master.
	CavesShard = "Caves"
	// ShardPort is the internal port the two shards talk over on localhost.
	ShardPort = 10888
	// CavesPortOffset keeps the Caves UDP port next to the Master one.
	CavesPortOffset = 1
	// TokenFile is where the server reads the Klei cluster token.
	TokenFile = "cluster_token.txt"
)

// InstallArgs returns the SteamCMD arguments that install the server into dir.
func InstallArgs(dir string) []string {
	return []string{
		"+force_install_dir", dir,
		"+login", "anonymous",
		"+app_update", AppID, "validate",
		"+quit",
	}
}

// ServerPath returns the 64-bit server binary inside an install directory.
func ServerPath(installDir string) string {
	return filepath.Join(installDir, filepath.FromSlash(Binary))
}

// ClusterDir returns the directory the server treats as -cluster.
func ClusterDir(inst instance.Instance) string {
	return filepath.Join(inst.DataDir, ConfDir, inst.Name)
}

// ShardDir returns the directory of the surface shard, the Master.
func ShardDir(inst instance.Instance) string {
	return filepath.Join(ClusterDir(inst), Shard)
}

// CavesDir returns the directory of the underground shard.
func CavesDir(inst instance.Instance) string {
	return filepath.Join(ClusterDir(inst), CavesShard)
}

// CavesPort returns the UDP port the Caves shard listens on.
func CavesPort(inst instance.Instance) int { return inst.Network.Port + CavesPortOffset }

// Args returns the launch arguments of the Master shard.
func Args(inst instance.Instance) []string {
	return shardArgs(inst, Shard, inst.Network.Port)
}

// shardArgs returns the launch arguments of one shard on one port.
func shardArgs(inst instance.Instance, shard string, port int) []string {
	args := []string{
		"-persistent_storage_root", inst.DataDir,
		"-conf_dir", ConfDir,
		"-cluster", inst.Name,
		"-shard", shard,
		"-players", strconv.Itoa(inst.MaxPlayers),
		"-port", strconv.Itoa(port),
	}
	if inst.Network.Mode == instance.NetworkOffline {
		args = append(args, "-offline")
	}
	return args
}

// Prepare writes the cluster files the server reads before it starts.
// It never overwrites the ini files: the server rewrites them and the player
// owns every setting gamehost does not model. The token is refreshed because
// a stale one is the usual reason an online cluster fails to register.
func Prepare(inst instance.Instance) error {
	if inst.Game != instance.GameDontStarveTogether {
		return fmt.Errorf("prepare %q: not a Don't Starve Together instance", inst.Name)
	}

	clusterDir, shardDir := ClusterDir(inst), ShardDir(inst)
	dirs := []string{clusterDir, shardDir}
	if inst.Caves {
		dirs = append(dirs, CavesDir(inst))
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create cluster directory %q: %w", dir, err)
		}
	}

	clusterINI := filepath.Join(clusterDir, "cluster.ini")
	if err := writeMissing(clusterINI, renderClusterINI(inst)); err != nil {
		return err
	}
	if inst.Caves {
		// A cluster created before Caves support has no shard section; add it
		// without touching anything the player wrote.
		if err := ensureShardSection(clusterINI); err != nil {
			return err
		}
		if err := writeMissing(filepath.Join(CavesDir(inst), "server.ini"), renderCavesINI(inst)); err != nil {
			return err
		}
	}
	if err := writeMissing(filepath.Join(shardDir, "server.ini"), renderServerINI(inst)); err != nil {
		return err
	}

	if inst.Network.Mode != instance.NetworkOnline {
		return nil
	}
	path := filepath.Join(clusterDir, TokenFile)
	if err := atomicfile.Replace(path, []byte(inst.ClusterToken+"\n"), 0o600); err != nil {
		return fmt.Errorf("write cluster token %q: %w", path, err)
	}
	return nil
}

// writeMissing writes a default file only when the player has none yet.
func writeMissing(path string, data []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("look for %q: %w", path, err)
	}
	if err := atomicfile.Replace(path, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}
	return nil
}

// ensureShardSection appends the shard settings the Caves shard needs when the
// file predates them. An existing section is left exactly as it is.
func ensureShardSection(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	if strings.Contains(string(data), "[SHARD]") {
		return nil
	}

	key, err := newClusterKey()
	if err != nil {
		return err
	}
	section := fmt.Sprintf("\n[SHARD]\nshard_enabled = true\nbind_ip = 127.0.0.1\nmaster_ip = 127.0.0.1\nmaster_port = %d\ncluster_key = %s\n", ShardPort, key)
	updated := append(append([]byte{}, data...), []byte(section)...)
	if err := atomicfile.Replace(path, updated, 0o644); err != nil {
		return fmt.Errorf("add the shard section to %q: %w", path, err)
	}
	return nil
}

// newClusterKey returns the shared secret the two shards authenticate with.
func newClusterKey() (string, error) {
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("generate cluster key: %w", err)
	}
	return hex.EncodeToString(key), nil
}

// renderClusterINI renders the settings shared by every shard of the cluster.
func renderClusterINI(inst instance.Instance) []byte {
	var b strings.Builder
	b.WriteString("[GAMEPLAY]\n")
	b.WriteString("game_mode = survival\n")
	fmt.Fprintf(&b, "max_players = %d\n", inst.MaxPlayers)
	b.WriteString("pvp = false\n\n")

	b.WriteString("[NETWORK]\n")
	fmt.Fprintf(&b, "cluster_name = %s\n", inst.World.Name)
	b.WriteString("cluster_description =\n")
	b.WriteString("cluster_password =\n")
	b.WriteString("cluster_intention = cooperative\n")
	b.WriteString("cluster_language = en\n")
	fmt.Fprintf(&b, "offline_cluster = %t\n\n", inst.Network.Mode == instance.NetworkOffline)

	b.WriteString("[MISC]\n")
	b.WriteString("console_enabled = true\n")
	return []byte(b.String())
}

// renderServerINI renders the settings of the master shard.
func renderServerINI(inst instance.Instance) []byte {
	return renderShardINI(inst.Network.Port, true, "", 27018, 27019)
}

// renderCavesINI renders the settings of the underground shard.
func renderCavesINI(inst instance.Instance) []byte {
	return renderShardINI(CavesPort(inst), false, CavesShard, 27020, 27021)
}

// renderShardINI renders one server.ini without ever touching the other.
func renderShardINI(port int, master bool, name string, masterPort, authPort int) []byte {
	var b strings.Builder
	b.WriteString("[NETWORK]\n")
	fmt.Fprintf(&b, "server_port = %d\n\n", port)

	b.WriteString("[SHARD]\n")
	fmt.Fprintf(&b, "is_master = %t\n", master)
	if name != "" {
		fmt.Fprintf(&b, "name = %s\n", name)
	}
	b.WriteString("\n")

	// The master uses the documented default Steam ports; the Caves shard
	// moves to 27020 and 27021.
	b.WriteString("[STEAM]\n")
	fmt.Fprintf(&b, "master_server_port = %d\n", masterPort)
	fmt.Fprintf(&b, "authentication_port = %d\n", authPort)
	return []byte(b.String())
}

// Installer exposes the SteamCMD side as a game.Installer.
type Installer struct{}

// AppID returns the Steam application of the dedicated server.
func (Installer) AppID() string { return AppID }

// InstallArgs returns the SteamCMD arguments that install the server into dir.
func (Installer) InstallArgs(dir string) []string { return InstallArgs(dir) }

// ServerPath returns the server binary inside an install directory.
func (Installer) ServerPath(installDir string) string { return ServerPath(installDir) }

// Runner exposes the launch contract as a game.Runner.
type Runner struct{}

// Launch returns the command line that starts the server.
// The binary needs its own directory as working directory for its libraries.
func (Runner) Launch(inst instance.Instance) (game.Launch, error) {
	launch := game.Launch{
		Path: ServerPath(inst.InstallDir),
		Args: Args(inst),
		Dir:  filepath.Dir(ServerPath(inst.InstallDir)),
	}
	if inst.Caves {
		launch.Extra = []game.Launch{{
			Path: ServerPath(inst.InstallDir),
			Args: shardArgs(inst, CavesShard, CavesPort(inst)),
			Dir:  launch.Dir,
		}}
	}
	return launch, nil
}

// Prepare writes the cluster files the server reads at startup.
func (Runner) Prepare(inst instance.Instance) error { return Prepare(inst) }

// SessionID is empty: Don't Starve Together has no join code.
func (Runner) SessionID(instance.Instance) string { return "" }

// Join reports the name the cluster shows in the server browser.
func (Runner) Join(inst instance.Instance) (string, string) {
	return "cluster", inst.World.Name
}
