# Don't Starve Together driver (M14)

Goal: a third game behind the same driver interface, without leaking a branch on the game name into
the CLI, the model or the lobby.

## Contract, verified before code

Primary sources: the Klei command line guide and the Don't Starve Wiki dedicated server guide,
checked against a real anonymous install of app `343050` (build `762172`, 4.4 GB).

- Binary `bin64/dontstarve_dedicated_server_nullrenderer_x64`, run with `bin64` as working directory.
- `-persistent_storage_root <data>` + `-conf_dir gamehost` + `-cluster <instance>` put the cluster at
  `data/gamehost/<instance>/`; the Master shard lives in `Master/`.
- `cluster.ini` and `Master/server.ini` are mandatory; `cluster_token.txt` is mandatory for an online
  cluster and ignored offline.
- `-players` overrides `[GAMEPLAY] max_players`; `-port` overrides `[NETWORK] server_port` (`10999`).
- An offline cluster (`offline_cluster = true`, `-offline`) is LAN only and its port must be
  10998–11018. Players join through the server browser by `cluster_name`, or by address and port.
- Caves is a second process (`-shard Caves`, next UDP port) that needs `[SHARD]` in `cluster.ini`
  and its own `Caves/server.ini` with `is_master = false`. The shards talk over UDP `10888`.
- Admins are Klei user ids (`KU_...`), one per line in `<cluster>/adminlist.txt`; there are no
  privileges and no display names.

## What was built

- `internal/dontstarvetogether`: install args, binary path, launch args, `Prepare`, and the cluster
  file renderers. `Prepare` creates `cluster.ini` and `server.ini` only when missing and rewrites
  `cluster_token.txt` with mode `0600` for an online cluster.
- `game.Preparer`: an optional driver interface, called by `prepareStart` and by the hidden
  `__supervise` entry point before `Launch`.
- `internal/instance`: `GameDontStarveTogether`, `NetworkOnline`/`NetworkOffline`, `ClusterToken`,
  and per-game validation (1–64 players, no Core Keeper or Project Zomboid fields, token required
  online, LAN port range offline, a `cluster.ini`-safe cluster name).
- `create`/`config`: `--cluster-token`, `--offline` and `--caves`, with the flag-owner table
  rejecting flags that belong to another game.
- `game.Launch.Extra`: additional processes that join the primary process group, so the one pid in
  `state.json`, the existing stop signal and systemd `KillMode=mixed` keep working. The supervisor
  merges both results and treats an unrequested shard exit as a crash.
- `game.AdminStore`: Core Keeper keeps `Admins.json` (Steam ID, privileges, name); Don't Starve
  Together keeps `adminlist.txt` (`KU_...` ids). The CLI and the lobby only see the interface, so
  the admin screen works for both games.
- Lobby: a `Cluster` choice (online/offline) and a token field that disappears offline, plus
  `show`/`list` output that never prints the token itself.
- Docs: `docs/design.md` contract and M14, README quick start, AGENTS traps.

## Log

- **D1** — Anonymous SteamCMD install of `343050` finished with `Success! App '343050' fully
  installed.` and placed both `bin/dontstarve_dedicated_server_nullrenderer` and the 64-bit
  `bin64/..._x64`.
- **D2** — First real start exposed a host dependency: exit `127`,
  `libcurl-gnutls.so.4: cannot open shared object file`. Fedora/Nobara ship only the OpenSSL
  `libcurl.so.4` and no package provides the gnutls soname. Resolved for the test with a local
  symlink on `LD_LIBRARY_PATH`; recorded in AGENTS as a runtime requirement rather than hidden.
- **D3** — Real offline cluster `dst` (world `Prueba DST`, 4 players, port 10999) started detached,
  generated the world (`World generated on build 762172 with save version: 5.27`), printed
  `Telling Client our new session identifier: D8835938996F494F`, and `status` reported the cluster
  name. `stop` exited `0`.
- **D4** — The server appended `[ACCOUNT] encode_user_path = true` to `Master/server.ini` and left
  the rest of the file alone, which is exactly why `Prepare` never overwrites it after the first
  write.
- **D5** — `backup` archived the 6.3 MB data directory into a 1.1 MB archive; `list` showed
  `dst  dontstarvetogether  4  offline:10999  Prueba DST`.
- **D6** — `gofmt -l .`, `go vet ./...` and `go test -race ./...` clean; the new driver and model
  have table-driven tests for args, file creation, idempotence, token permissions and validation.
- **C1** — Caves started as two processes with the same process group (`PGID 762`): the Master on
  UDP 10999 and `-shard Caves` on 11000. The log reached
  `[Shard] Secondary shard Caves(254892845) connected: [LAN] 127.0.0.1` and
  `World 254892845(Caves) is now connected`; the server appended its shard `id` to
  `Caves/server.ini` without touching the generated settings.
- **C2** — `stop` initially returned when the Master died, leaving the supervisor (and the lock)
  alive for a moment while Caves exited. `stop` now waits for the whole process group, so the next
  `admin` or `backup` never races the lock.
- **C3** — Killing the Caves shard with `SIGKILL` took the instance down: the Master shut down,
  `status` reported `crashed` with exit `-1`, and no game process stayed behind.
- **C4** — Klei admins from the CLI and from the lobby adapter: `admin add caves KU_QDNly1sC`
  wrote the id to `data/gamehost/caves/adminlist.txt`, `admin list` showed it and `remove` emptied
  the file. `backup` archived 19.4 MiB of both shards' saves.
- **D7** — Online cluster verified with the player's Klei token, in a throwaway data root: the log
  showed `Token retrieved from: .../cluster_token.txt`, `Received (KU_...) from TokenPurpose`,
  `Telling Client our new session identifier: E55C3FFDD251D8A7` and
  `Server registered via geo DNS in us-east-1`; the token file was `0600`. The throwaway root and
  the token file were deleted afterwards, and the token never entered this repository.
