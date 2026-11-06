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

## What was built

- `internal/dontstarvetogether`: install args, binary path, launch args, `Prepare`, and the cluster
  file renderers. `Prepare` creates `cluster.ini` and `server.ini` only when missing and rewrites
  `cluster_token.txt` with mode `0600` for an online cluster.
- `game.Preparer`: an optional driver interface, called by `prepareStart` and by the hidden
  `__supervise` entry point before `Launch`.
- `internal/instance`: `GameDontStarveTogether`, `NetworkOnline`/`NetworkOffline`, `ClusterToken`,
  and per-game validation (1–64 players, no Core Keeper or Project Zomboid fields, token required
  online, LAN port range offline, a `cluster.ini`-safe cluster name).
- `create`/`config`: `--cluster-token` and `--offline`, with the flag-owner table rejecting flags
  that belong to another game.
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
- **D7** — Online cluster verified with the player's Klei token, in a throwaway data root: the log
  showed `Token retrieved from: .../cluster_token.txt`, `Received (KU_...) from TokenPurpose`,
  `Telling Client our new session identifier: E55C3FFDD251D8A7` and
  `Server registered via geo DNS in us-east-1`; the token file was `0600`. The throwaway root and
  the token file were deleted afterwards, and the token never entered this repository.
