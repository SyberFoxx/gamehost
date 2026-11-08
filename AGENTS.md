# AGENTS.md

## Context

`gamehost` is a terminal-first manager for dedicated game servers, written in Go 1.26.
Supported games: Core Keeper (`1963720`), Project Zomboid (`380870`) and Don't Starve Together
(`343050`).
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

Supported today: Core Keeper (`1963720`), Project Zomboid (`380870`) and Don't Starve Together
(`343050`). The Don't Starve Together driver creates `cluster.ini`, `Master/server.ini` and, for an
online cluster, `cluster_token.txt` (mode `0600`) before the launch through `game.Preparer`; it
writes the ini files only when they are missing, because the server appends its own sections and
the player owns the rest. With `--caves` it also adds the `[SHARD]` section once, writes
`Caves/server.ini`, and launches a second process in the Master's process group.
A game is one driver package plus one entry in `internal/games`; verify its contract in the official
documentation and record the facts in `docs/design.md` before writing flags.

Candidates, easiest first:

| Game | Install | What the driver needs |
| --- | --- | --- |
| Terraria (`105600`) | SteamCMD anonymous (verify) | `-world`, `-worldname`, `-autocreate`, `-port`, `-maxplayers`, `-password`; the first start creates the world; join by IP:7777 |
| Valheim (`896660`) | SteamCMD anonymous | `valheim_server.x86_64` with `-name -port -world -password -public`; needs `SteamAppId=892970` and `steamclient.so`; also needs `Env` in `game.Launch` |
| Palworld (`2394010`) | SteamCMD anonymous plus app `1007` libs | `PalServer.sh`, `PalWorldSettings.ini`, 8211 UDP plus the 27015 query port; memory hungry |
| tModLoader (`1281930`) | SteamCMD anonymous | Terraria with mods: same flags plus the mod loader |
| Minecraft Java | not on Steam: Mojang or Paper jar, EULA, Java runtime | needs a general install strategy instead of SteamCMD args, `server.properties`, RCON |
| Factorio (`427520`) | authenticated SteamCMD login, or the headless tarball from factorio.com | `factorio --start-server`, `server-settings.json`; anonymous SteamCMD does not work, so the installer needs a login or tarball path |

## Testing

- Table-driven tests with `t.Run`; `t.Parallel` when there is no shared state.
- Always run `go test -race ./...`.
- Filesystem tests use `t.TempDir()`, never the repository.
- Fakes are hand-written against small interfaces.
- The lobby is tested by sending messages to `Model.Update` and reading `View().Content`; no
  terminal and no golden files are needed, and no test may require a TTY.
- A screen with a streamed backend is tested with a buffered channel the test fills and closes, then
  by pumping `Update` until `Done`. Do not start a real SteamCMD or a real follower in a unit test.
- Real SteamCMD or server runs use the `//go:build integration` tag and are not part of the
  default suite.

## Known traps

- `-worldmode` takes names (`Normal`, `Hard`, `Creative`, `Casual`); `ServerConfig.json` stores
  numbers. Never pass the number to the flag.
- Game IDs are 15–28 alphanumeric characters that exclude `Y`, `y`, `x`, `0` and `O`.
- `GameInfo.txt` and `GameID.txt` live in the install directory, not in the data directory.
- World saves are `<datapath>/worlds/<index>.world.gzip`; the server also writes
  `ServerConfig.json`, `Admins.json` and `PlayerBans.json` there, with `.pugbackup` copies.
- The verified Core Keeper build (2026-11-06) starts with `-batchmode` and opens no window; two
  polls of the X session during startup and one while running found nothing. Keep `-batchmode` and
  the `DISPLAY` export anyway: a build that initializes graphics needs both.
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
- `backup`, `restore` and `admin add|remove` require a stopped server; Core Keeper rewrites world
  files and `Admins.json` in place.
- `Admins.json` is `{"adminList":[{"index":1,"privileges":2,"name":"...","steamId":765...}]}`.
  `privileges` is `2` for full and `1` for limited, `index` is one-based and must be renumbered
  after a removal, and `steamId` is a JSON number, not a string. Keep `internal/admins` as the only
  writer and never hand-edit the file while the server runs.
- State-changing HTTP requests must pass the same-origin check in `internal/httpapi`: an `Origin`
  that matches the request host, or no browser headers, and never `Sec-Fetch-Site: cross-site`.
  Do not add a mutating route outside the `ServeHTTP` guard.
- `update` also refuses a running instance and writes `backups/pre-update_<timestamp>.tar.gz` before
  SteamCMD starts; do not weaken either check to make an update convenient.
- Project Zomboid reads `WorkshopItems=` (numeric Steam Workshop ids) and `Mods=` (folder names) from
  the top of `Server/<name>.ini`. `mods list` works while running; add and remove need a stopped
  server. The ini only exists after the first start, and a missing file is a clear error.
- `game.AdminStore` and `game.ModStore` are optional driver interfaces; the CLI and the lobby assert
  them instead of branching on the game name. Project Zomboid has no admin list yet, and only
  Project Zomboid has a mod list.
- `serve` binds `127.0.0.1:8080` and has no authentication; keep it on loopback and let a reverse
  proxy own access control.
- The restore extractor rejects any tar entry that is not local to the target directory; keep that
  check when touching `internal/backup`.
- World data lives under `-datapath`, not next to the binary. Back up that directory.
- Each instance owns `servers/<name>/run.lock`. `serve` and every data command (`backup`, `update`,
  `restore`, `import-world`) take it with `flock(LOCK_EX|LOCK_NB)`; do not remove the file, and do
  not replace the lock with a state-file check. The kernel releases it when a process dies.
- `import-world` accepts any non-empty `*.world.gzip` regular file, writes into the instance's
  `World.Index` slot, and archives `data/` first when it is not empty. Core Keeper saves do not
  start with the gzip magic their extension suggests; never re-add a header check.
- `config` only edits `instance.json` for values the launch flags and the driver consume (players,
  mode, port, password, game ID, admin password, Project Zomboid memory and autosave, cluster token,
  offline, caves, backup window). Never write `ServerConfig.json`, `Admins.json` or the Project
  Zomboid ini from it; the driver and the server own those files.
- `config` splits its settings by who reads them. Launch flags still require a stopped server. The
  backup schedule applies live because the supervisor reloads the instance every tick. A Project
  Zomboid heap or autosave change is validated (pure, no file write) and then the running server is
  restarted by `config` itself; a stopped server only gets the new value saved.
- The one exception for the Project Zomboid ini is `mods add|remove`: it rewrites only the
  `WorkshopItems=` and `Mods=` lines, in place, byte for byte, and only while stopped. The second is
  the autosave interval, which `game.Preparer` writes into `SaveWorldEveryMinutes=` before a launch
  (line replacement only, skipped while the ini does not exist yet). Do not generalize those writes
  to other keys.
- The scheduled backup is a maintenance window: stop, archive, prune, start again. The supervisor
  owns the loop, so `start --detach`, the lobby and the systemd unit all get it; `serve` and the
  panel run no scheduler and there is no systemd timer. A failed archive must still restart the
  server, and a user stop during the window wins (the phase is no longer `stopping`).
  `--backup-every` is off by default.
- Retention prunes two classes independently: timestamped archives keep `keep` newest, and the
  `pre-*` safety archives keep their own `keep`, both ordered by modification time. Never fold them
  into one count, or a burst of safety archives deletes the routine snapshots.
- The systemd unit exports the `DISPLAY` it was installed from. The verified build opens no window
  with `-batchmode`, but an older or future one may, and a headless boot then needs `Xvfb` plus
  `DISPLAY=:99 gamehost service install ...` or the unit fails before reaching its log.
- `service install` renders the units with `internal/service` and is the only caller of
  `systemctl`. Unit files are rendered text: escape `%` and `$`, quote paths with spaces, and keep
  `KillMode=mixed` so gamehost owns the game's shutdown instead of systemd signaling the whole
  cgroup. `Install` always removes a backup unit or timer left by an older version.
- `POST /api/instances/{name}/restore` accepts only a bare archive name from the backup directory.
  Keep that check when touching `internal/httpapi`: it is the API's path-traversal guard.
- Setting `-port` switches the server from Steam relay (SDR) to direct connect and requires
  port forwarding; leave it unset for a small friend group.
- Project Zomboid runs `start-server.sh` from its install directory and takes `-servername`,
  `-adminpassword`, `-port`, `-udpport` and `-cachedir`; its world data is `-cachedir`, defaulting
  to `~/Zomboid` (`Server/<name>.ini`, `Saves/Multiplayer/<name>`, `db`, `Logs`).
- Project Zomboid prompts for an admin password on first start, so a headless launch must pass
  `-adminpassword`; max players and the join password live in the ini, not in flags.
- Project Zomboid has no game ID: players join by address and UDP port (`16261`, direct `16262`).
- Project Zomboid saves the world on the interval in `SaveWorldEveryMinutes=` of its ini. The value
  lives in `instance.json` as `autosave_minutes`, and the ini only exists after the first start, so
  the interval applies from the second launch and the driver skips the write when the file is
  missing. The live server keeps the value across stop/start; it never resets it.
- Project Zomboid reads its JVM heap from `-Xmx` in `vmArgs` of `ProjectZomboid64.json`, and
  `update` restores the file SteamCMD ships. `--memory` is stored in `instance.json` and the driver
  rewrites only that entry through `game.Preparer` before every launch, keeping every other key and
  vmArg. Never touch `-Xms` or other vmArgs from gamehost, and never write the json from `config`.
  `instance.New` stores the shipped default (`8g`) when the user gives no value, so a Project
  Zomboid instance always carries an explicit heap; a value larger than `MemTotal` is refused before
  the launcher runs, and `game.Validator` lets `config` refuse it before stopping a running server.
- The `start-server.sh` script ships from SteamCMD; if it loses the execute bit, `start` fails with
  `permission denied` until it is `chmod +x`.
- Don't Starve Together is Steam app `343050`; `bin64/dontstarve_dedicated_server_nullrenderer_x64`
  must run with `bin64` as working directory. `-persistent_storage_root <data>`, `-conf_dir gamehost`
  and `-cluster <instance>` put the cluster at `data/gamehost/<instance>/`.
- An online DST cluster needs a Klei token from `accounts.klei.com`; an offline cluster
  (`offline_cluster = true` plus `-offline`) needs no token but its UDP port must be 10998–11018.
  `-players` and `-port` override `cluster.ini` and `server.ini`, so gamehost passes both.
- The DST server appends sections such as `[ACCOUNT] encode_user_path = true` to `server.ini`;
  never rewrite that file, and never treat the appended line as drift.
- The DST binary needs `libcurl-gnutls.so.4`, which Fedora and Nobara do not package; the server
  exits 127 with `error while loading shared libraries` until it is on the loader path.
- DST has no game ID: the cluster is found in the server browser by its `cluster_name`, or by
  address and UDP port.
- The Caves shard is a second process with `-shard Caves` and the next UDP port. Both shards share
  the Master's process group, so `state.json` keeps one pid and one stop signal reaches both; a
  shard that dies unasked stops the instance instead of leaving dead portals behind.
- `cluster.ini` gets its `[SHARD]` section only once (with `cluster_key`, `master_port 10888` and
  `127.0.0.1`); never rewrite the rest of the file. `Caves/server.ini` uses Steam ports `27020` and
  `27021`.
- DST admins live in `<cluster>/adminlist.txt`, one `KU_...` id per line, and the server reads it at
  startup. There are no privileges and no display names; reject `--limited` instead of writing a
  file the server would ignore. `game.AdminStore` is the only interface the CLI and the lobby use,
  with Core Keeper and DST implementing it.
- The lobby only opens when stdin and stdout are terminals, and the gate lives in `app.Run` so
  `main` stays a dispatcher. Do not move it into `main`, and keep the stdin check: a redirected
  stdin would otherwise open an invisible UI. The lobby is the front end of every command, but the
  commands stay: systemd units, `serve` and scripts have no keyboard to press `Enter`.
- `internal/tui` is a screen stack: each screen implements `Init`, `Update` and `View`, and the
  root model owns navigation, the footer note, the help overlay and the confirm dialog. A new
  capability is a new screen pushed by the lobby, not a field on the root model.
- Long operations return a `<-chan tui.Event` and the screen redeems one event per command. Never
  write to the model from a backend goroutine, and cancel the screen's context when it closes:
  `Esc` on `Files` or `Logs` must stop the child SteamCMD or follower.
- Forms send a `tui.Settings` of strings and `internal/app` parses it with the same helpers, defaults
  and messages as `create` and `config`. Do not parse or validate in `internal/tui`.
- The web panel screen starts the same handler `serve` uses, owns the stop function and the boot
  start. It runs no backup scheduler; the supervisor of each instance owns that. The lobby context
  shuts the listener down, so
  `servePanel` is the only place that listener is created inside the lobby. Its output goes to
  `io.Discard`: never write to stdout while the alternate screen is up.
- The log screen restarts its follower when the tail size changes, and every streamed message
  carries a sequence number. Keep it: a cancelled channel that closes late would otherwise mark the
  new stream as finished.
- The lobby reaches the per-run options with forms, not hidden constants: `Backup` asks for the
  retention (`--keep`) and prefills the instance value, the Backups screen `e` edits the schedule
  through `ServerBackend.Configure`; `Systemd` has an options form for `--dir`, `--binary` and
  `--systemctl`. A new flag that changes behaviour needs the same treatment or an explicit note
  that it is command-only.
- Foreground `start` has no lobby screen: the alternate screen owns the terminal. The lobby always
  uses the detached supervisor, the same path as `start --detach`.
- `internal/tui` imports nothing from this project; it receives a `Backend` whose fields are
  functions. Do not import `store` or `instance` there, and never shell out to the `gamehost`
  binary: the adapter in `internal/app/tui.go` reuses `startDetached`, `stopInstance`,
  `backupInstance` and `logs.Tail`, which is what keeps the lobby and the CLI honest.
- Bubble Tea v2 and Lip Gloss v2 are `charm.land/bubbletea/v2` and `charm.land/lipgloss/v2`. The
  `github.com/charmbracelet/...` paths still resolve, but to v1: do not "fix" the import.
- The lobby owns the alternate screen. Never write to stdout from inside it (`fmt.Println` in a
  helper corrupts the frame); report through the model note or the error footer.
- Terminal detection uses `charmbracelet/x/term`, not `os.ModeCharDevice`: `/dev/null` is a
  character device too, and pointing the lobby at it hides the UI.
