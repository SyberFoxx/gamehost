# AGENTS.md

## Context

`gamehost` is a terminal-first manager for dedicated game servers, written in Go 1.26.
Supported games: Core Keeper (Steam app `1963720`) and Project Zomboid (Steam app `380870`).
Module path: `github.com/syberfoxx/gamehost`.
Entry point: `cmd/gamehost`. Design and roadmap: `docs/design.md`.

## Commands

- Build: `make build` or `go build ./...`
- Test: `make test` or `go test -race ./...`
- Vet: `make lint` or `go vet ./...`
- Format: `make fmt` or `gofmt -l -w .`
- Run: `go run ./cmd/gamehost version`

## Style and conventions

- `gofmt` is non-negotiable; no manual formatting diffs.
- Prefer the standard library. A new dependency needs a one-line justification.
- Interfaces: 1–3 methods, declared where consumed. No interface for a single implementation
  without a consumer.
- Constructors take dependencies as interfaces, return concrete types.
- `context.Context` is the first parameter for any function that blocks or does I/O.
- Wrap errors with `%w` and the operation that failed: `fmt.Errorf("start instance %q: %w", name, err)`.
- Sentinel errors for conditions callers must distinguish; compare with `errors.Is`.
- `main` stays minimal: parse nothing, call `app.Run`, map errors to exit codes.
- One-line comments that state purpose, lowercase, no trailing period.
- No `init`, no mutable package state, no `util`/`common` packages.

## Work rules

- Read `docs/design.md` and the files you will touch before editing.
- Verify game behavior against the documented contract; do not invent flags.
  Core Keeper: `github.com/escapingnetwork/core-keeper-dedicated` (`scripts/compile-parameters.sh`).
  Project Zomboid: `pzwiki.net/wiki/Dedicated_server` and `pzwiki.net/wiki/Startup_parameters`.
- Game behavior lives behind `internal/game`: add a driver package and one entry in
  `internal/games`, never a `switch` on the game name in the CLI.
- Changes to launch flags, paths or the state machine must update `docs/design.md` in the same change.
- One milestone per work unit: code, tests and docs together.

## Games and drivers

Supported today: Core Keeper (Steam app `1963720`) and Project Zomboid (`380870`).
A game is one driver package plus one entry in `internal/games`; verify its contract in the official
documentation and record the facts in `docs/design.md` before writing flags.

Candidates, easiest first:

| Game | Install | What the driver needs |
| --- | --- | --- |
| Terraria (`105600`) | SteamCMD anonymous (verify) | `-world`, `-worldname`, `-autocreate`, `-port`, `-maxplayers`, `-password`; the first start creates the world; join by IP:7777 |
| Valheim (`896660`) | SteamCMD anonymous | `valheim_server.x86_64` with `-name -port -world -password -public`; needs `SteamAppId=892970` and `steamclient.so`; also needs `Env` in `game.Launch` |
| Palworld (`2394010`) | SteamCMD anonymous plus app `1007` libs | `PalServer.sh`, `PalWorldSettings.ini`, 8211 UDP plus the 27015 query port; memory hungry |
| Don't Starve Together (`343050`) | SteamCMD anonymous | requires a cluster token up front and `cluster.ini`; the token breaks a one-shot `create` |
| tModLoader (`1281930`) | SteamCMD anonymous | Terraria with mods: same flags plus the mod loader |
| Minecraft Java | not on Steam: Mojang or Paper jar, EULA, Java runtime | needs a general install strategy instead of SteamCMD args, `server.properties`, RCON |
| Factorio (`427520`) | authenticated SteamCMD login, or the headless tarball from factorio.com | `factorio --start-server`, `server-settings.json`; anonymous SteamCMD does not work, so the installer needs a login or tarball path |

## Testing

- Table-driven tests with `t.Run`; `t.Parallel` when there is no shared state.
- Always run `go test -race ./...`.
- Filesystem tests use `t.TempDir()`, never the repository.
- Fakes are hand-written against small interfaces.
- Real SteamCMD or server runs use the `//go:build integration` tag and are not part of the
  default suite.

## Known traps

- `-worldmode` takes names (`Normal`, `Hard`, `Creative`, `Casual`); `ServerConfig.json` stores
  numbers. Never pass the number to the flag.
- Game IDs are 15–28 alphanumeric characters that exclude `Y`, `y`, `x`, `0` and `O`.
- `GameInfo.txt` and `GameID.txt` live in the install directory, not in the data directory.
- World saves are `<datapath>/worlds/<index>.world.gzip`; the server also writes
  `ServerConfig.json`, `Admins.json` and `PlayerBans.json` there, with `.pugbackup` copies.
- The server needs a graphics display even with `-batchmode`: on a desktop it opens a window.
- SteamCMD ships a 32-bit client (`linux32/steamcmd`); a minimal distribution without 32-bit
  runtime libraries fails before reaching its prompt.
- SteamCMD self-updates on every invocation; do not treat output as silence or assume fast startup.
- The server must run with its install directory as working directory; `GameID.txt` is written there.
- `start --detach` re-executes `gamehost __supervise <name> --log <path>` with `Setsid`; stdout of
  that child is the log file. The command is hidden on purpose and must keep working without a
  terminal.
- A zombie answers `kill(pid, 0)`. Liveness checks read `/proc/<pid>/stat` and treat `Z` as dead,
  which is also why the project is Linux-only.
- A server killed from outside by `SIGTERM`/`SIGINT` counts as a clean stop; do not report it as a
  crash.
- `backup` and `restore` require a stopped server; Core Keeper rewrites world files in place.
- `update` also refuses a running instance and writes `backups/pre-update_<timestamp>.tar.gz` before
  SteamCMD starts; do not weaken either check to make an update convenient.
- `serve` binds `127.0.0.1:8080` and has no authentication; keep it on loopback and let a reverse
  proxy own access control.
- The restore extractor rejects any tar entry that is not local to the target directory; keep that
  check when touching `internal/backup`.
- World data lives under `-datapath`, not next to the binary. Back up that directory.
- Each instance owns `servers/<name>/run.lock`. `serve` and every data command (`backup`, `update`,
  `restore`, `import-world`) take it with `flock(LOCK_EX|LOCK_NB)`; do not remove the file, and do
  not replace the lock with a state-file check. The kernel releases it when a process dies.
- `import-world` accepts only `*.world.gzip` with a gzip header, writes into the instance's
  `World.Index` slot, and archives `data/` first when it is not empty.
- Setting `-port` switches the server from Steam relay (SDR) to direct connect and requires
  port forwarding; leave it unset for a small friend group.
- Project Zomboid runs `start-server.sh` from its install directory and takes `-servername`,
  `-adminpassword`, `-port`, `-udpport` and `-cachedir`; its world data is `-cachedir`, defaulting
  to `~/Zomboid` (`Server/<name>.ini`, `Saves/Multiplayer/<name>`, `db`, `Logs`).
- Project Zomboid prompts for an admin password on first start, so a headless launch must pass
  `-adminpassword`; max players and the join password live in the ini, not in flags.
- Project Zomboid has no game ID: players join by address and UDP port (`16261`, direct `16262`).
- The `start-server.sh` script ships from SteamCMD; if it loses the execute bit, `start` fails with
  `permission denied` until it is `chmod +x`.
