<!-- TODO(banner): uncomment and add the image at docs/assets/banner.png
<p align="center">
  <img src="docs/assets/banner.png" alt="gamehost" width="100%">
</p>
-->

# gamehost

[![CI](https://github.com/SyberFoxx/gamehost/actions/workflows/ci.yml/badge.svg)](https://github.com/SyberFoxx/gamehost/actions/workflows/ci.yml)

Terminal-first manager for dedicated game servers, written in Go. The first supported game is
**Core Keeper**: install, configure, run, inspect and back up a server for a small friend group.

The project is also a structured Go learning path. Every milestone adds one real capability and the
language features it requires: `os/exec`, process groups, signals, atomic writes, `net/http`,
concurrency and tests with `-race`.

<!-- TODO(screenshots): add docs/assets/screenshot-status.png and a short caption
<p align="center">
  <img src="docs/assets/screenshot-status.png" alt="gamehost status" width="720">
</p>
-->

## Status

| Milestone | Capability | State |
| --- | --- | --- |
| M0 | `doctor`: environment probes | done |
| M1 | `install` / `update`: SteamCMD downloads | done |
| M2 | `create` / `list` / `show`: instances on disk | done |
| M3 | `start` / `status`: process supervision, graceful shutdown | done |
| M3b | `start --detach` / `stop`: background supervision | done |
| M4 | `logs` / `logs -f`: tail and follow | done |
| M5 | `backup` / `restore` | done |
| M6 | `serve`: HTTP API and web panel | done |
| M7 | second game driver | done |
| M8 | operation: `import-world`, per-instance lock, log retention | done |
| M9 | `config`, API backups and scheduled backup windows | done |
| M10 | operators: `admin`, cross-site guard, `serve --start` | done |

## Features

- Installs the dedicated server files with SteamCMD, anonymously, into an isolated directory.
- Keeps configuration and world data outside the repository, under a single `GAMEHOST_HOME` root.
- Validates the launch contract before touching a process: world mode names, game ID charset,
  player limits, network mode.
- Runs the server in the foreground or detached, with a clean `SIGTERM` → grace period → `SIGKILL`
  shutdown.
- Refuses a second server on the same world, and stops a detached one from any terminal.
- Persists the runtime phase so another terminal can see what is happening.
- Tees server output to both the terminal and a per-run log file.
- Archives the world into a compressed tar, prunes old copies and restores safely, keeping the
  previous data as a pre-restore backup.
- Serves a small web panel and a JSON API with live status, start/stop controls and an SSE log
  stream, embedding the page in the binary.
- Runs two games behind one driver interface: Core Keeper and Project Zomboid.
- Imports an existing `.world.gzip` save into an instance, archiving the current world first.
- Holds a per-instance lock, so two servers or a backup and a start can never share one world.
- Keeps the newest ten run logs and prunes the rest.
- Changes the settings the launch flags express with `config`, without editing JSON by hand.
- Lists, creates and restores backups from the API and the panel, not only the terminal.
- Runs an opt-in backup window from `serve`: stop the server, archive, prune, start it again.
- Edits the Core Keeper admin list from the terminal, with Steam ID validation and the server's
  own file shape.
- Starts instances when the daemon boots, so one systemd unit brings the server back after a
  reboot.
- Rejects cross-site writes to the API: another page open in your browser cannot stop or restore a
  world.

## Requirements

- Linux (the supervisor uses POSIX process groups and `/proc` to detect zombies)
- Go 1.26 or newer to build
- SteamCMD, looked up in this order: `GAMEHOST_STEAMCMD`, `PATH`,
  `~/.local/share/steamcmd/steamcmd.sh`
- A writable data directory, `~/.local/share/gamehost` by default

## Install

```bash
git clone https://github.com/SyberFoxx/gamehost.git
cd gamehost
make build
./bin/gamehost doctor
```

`doctor` checks the platform, the data directory, SteamCMD and the display, and exits non-zero when
the environment cannot run servers yet.

```
root: /home/ramses/.local/share/gamehost

  ok  platform   linux/amd64
  ok  data root  /home/ramses/.local/share/gamehost
  ok  steamcmd   /home/ramses/.local/share/steamcmd/steamcmd.sh
  ok  display    :0

environment ready
```

## Quick start

```bash
gamehost create corekeeper --max-players 5 --world-name "Cueva Compartida"
gamehost install corekeeper
gamehost start corekeeper --detach
```

```
corekeeper is running in the background (pid 326296)
  logs: /home/ramses/.local/share/gamehost/servers/corekeeper/logs/2026-11-04_14-01-14.log
  stop: gamehost stop corekeeper
```

In another terminal:

```bash
gamehost status corekeeper
gamehost list
gamehost logs corekeeper -f   # follow the server output
gamehost stop corekeeper
gamehost backup corekeeper    # archive the world
```

```
$ gamehost status corekeeper
instance  corekeeper
phase     running
pid       4183021
uptime    1m20s
game id   K85KQswu7GWLsQB
```

`status` reads the session id the server writes to `GameInfo.txt` next to its binary, so friends can
join with it. `start` records it in `state.json` when the run ends.

`config` prints the settings with no flags and changes them with flags:

```bash
gamehost config corekeeper                     # print what the launcher will use
gamehost config corekeeper --max-players 5 --backup-every 24h
```

It refuses while the server runs, because the server reads these values at launch. The world name
and seed are fixed after creation: the save already exists. For Project Zomboid, max players and
the join password stay in its ini.

`start` without flags keeps the server in the foreground with its output on the terminal; Ctrl+C
stops it. `start --detach` hands the server to a supervisor in its own session and returns, so the
server survives closing the terminal. `stop` signals the whole process group, waits up to 20
seconds for a clean save, then escalates to `SIGKILL`.

Without `--port` the server uses the Steam relay network, so no router configuration is needed.
Passing `--port 27015` switches it to direct connections, which requires opening UDP ports.

### Scheduled backups

A backup needs a stopped server, so a schedule is a maintenance window:

```bash
gamehost config corekeeper --backup-every 24h --backup-keep 5
gamehost serve                       # keep this running, for example under systemd
```

When the interval elapses, `serve` stops the server, writes `backups/<timestamp>.tar.gz`, prunes
archives beyond the retention and starts the server again. A stopped server is only archived, a
failed archive never leaves the server down, and `--backup-every off` disables the window.

### Admins

The server keeps its admin list in `data/Admins.json` and rewrites it on exit, so edit it with the
server stopped:

```bash
gamehost stop corekeeper
gamehost admin add corekeeper 76561198012345678 --name "Ramsés"
gamehost admin add corekeeper 76561198012345679 --name "Amigo" --limited
gamehost admin list corekeeper
gamehost admin remove corekeeper 76561198012345679
gamehost start corekeeper --detach
```

A Steam ID64 is the 17-digit number that starts with `7656119`. `full` (privileges `2`) can use
every admin command; `--limited` stores privileges `1`, the level the game marks as limited.
`admin list` works while the server runs; `add` and `remove` refuse until it is stopped.

### Autostart after a reboot

`start --detach` survives closing the terminal, not a reboot. One systemd user unit covers both
the autostart and the backup window, because `serve` already owns stop and start:

```ini
# ~/.config/systemd/user/gamehost.service
[Unit]
Description=gamehost: Core Keeper server and its backup window
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%h/proyectos/gamehost/bin/gamehost serve --start corekeeper
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now gamehost.service
loginctl enable-linger "$USER"      # user units start at boot, before you log in
journalctl --user -u gamehost -f    # daemon messages; the game log stays in the instance
```

Point `ExecStart` at wherever `go build` left the binary, and repeat `--start` for more instances.
With `--backup-every` configured, the same process archives the world on schedule. If the game
server crashes outside a backup window, `gamehost status` reports it and `gamehost start` brings
it back; `serve` does not restart a crashed instance on its own.

### Importing an existing world

A world created in single player, or copied from another server, can be dropped into an instance:

```bash
gamehost stop corekeeper
gamehost import-world corekeeper ~/Descargas/Cueva.world.gzip
gamehost start corekeeper --detach
```

`import-world` accepts any non-empty `.world.gzip` file, archives the current `data/` when it is
not empty, and copies the save into the instance's slot (the `world` index shown by
`gamehost show`). Client saves live in
`~/.config/unity3d/Pugstorm/Core Keeper/Steam/<steam-id>/worlds/<slot>.world.gzip`. Despite the
name, current Core Keeper saves are not plain gzip streams, so the server decides whether a save
is valid; the pre-import archive is the way back.

The imported save loads in the instance's world slot and takes its name from the instance. Rename it
with the server stopped:

```bash
gamehost config corekeeper --world-name Partidita
```

A Project Zomboid server uses the same commands with `--game`:

```bash
gamehost create zomboid --game projectzomboid --world-name base --admin-password "cambia esto"
gamehost install zomboid
gamehost start zomboid --detach
```

Project Zomboid has no join code: `status` reports the UDP port to add in the Favorites tab, and
max players and the join password stay in `data/Server/base.ini`.

## Verifying a real Core Keeper server

The test suite stops at the SteamCMD download. Once per machine, walk the whole path:

1. `gamehost doctor` reports every check `ok`.
2. `gamehost create corekeeper --max-players 4 --password change-me`.
3. `gamehost install corekeeper` ends with the binary path and no SteamCMD error.
4. `gamehost start corekeeper --detach`, then `gamehost logs corekeeper -f` until the session
   starts.
5. Join with the id from `gamehost status corekeeper`, play a minute so the world saves.
6. `gamehost stop corekeeper` prints one stopped line with exit `0`, not a signal.
7. `gamehost backup corekeeper` writes an archive whose size grows with the world.
8. Move `data/worlds/0.world.gzip` away, `gamehost restore corekeeper <archive>` and start again:
   the world is back with its builds.

## Commands

| Command | Purpose |
| --- | --- |
| `gamehost doctor` | check platform, data directory, SteamCMD and display |
| `gamehost create <name> [flags]` | create an instance and its directories |
| `gamehost config <name> [flags]` | print or change the settings the launch flags express (stopped only) |
| `gamehost install <name>` | download the server files with SteamCMD |
| `gamehost update <name>` | back up the world, then re-download and validate the server files (stopped only) |
| `gamehost start <name> [--detach]` | run the server, in the foreground or in the background |
| `gamehost stop <name>` | stop a running server, waiting for a clean save |
| `gamehost logs <name> [-n N]` | print the last `N` lines (default 100) of the newest log |
| `gamehost logs <name> -f` | follow the log until Ctrl+C |
| `gamehost import-world <name> <file>` | copy an existing `.world.gzip` save into the instance slot (stopped only) |
| `gamehost backup <name> [-keep N]` | archive `data/`, keeping the newest `N` (default 5) |
| `gamehost restore <name> <archive>` | replace `data/` from a backup, after saving the current one |
| `gamehost admin list <name>` | print the admins stored in `data/Admins.json` |
| `gamehost admin add <name> <steamid> [--name NAME] [--limited]` | grant admin rights (stopped only) |
| `gamehost admin remove <name> <steamid>` | drop admin rights (stopped only) |
| `gamehost serve [--addr]` | serve the HTTP API and web panel (default `127.0.0.1:8080`) |
| `gamehost serve --start <name>` | start that instance when the daemon boots; repeatable |
| `gamehost status <name>` | report phase, pid, uptime and join information |
| `gamehost list` | list instances |
| `gamehost show <name>` | print the full configuration of one instance |
| `gamehost version` | print the build version |

`create` flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--game` | `corekeeper` | `corekeeper` or `projectzomboid` |
| `--max-players` | `8` | player limit, 1–8 (Core Keeper only) |
| `--world-name` | per game | world name, or the Project Zomboid server name (lowercase letters, digits, `-` and `_`) |
| `--seed` | random | world seed, only used on first creation (Core Keeper only) |
| `--mode` | `normal` | `normal`, `hard`, `creative` or `casual` (Core Keeper only) |
| `--port` | `0` | direct-connect port; `0` keeps the Steam relay (Core Keeper) or uses `16261` (Project Zomboid) |
| `--udp-port` | game port + 1 | Project Zomboid direct port (Project Zomboid only) |
| `--password` | none | join password, at most 28 characters (Core Keeper only) |
| `--admin-password` | required | Project Zomboid admin password (Project Zomboid only) |
| `--game-id` | generated | fixed game ID, 15–28 characters without `Y y x 0 O` (Core Keeper only) |
| `--backup-every` | off | scheduled backup interval such as `24h`; needs `gamehost serve` running |
| `--backup-keep` | `5` | scheduled archives to keep, oldest removed first |

Flags work before or after the instance name.

## Web panel

```bash
gamehost serve
# serving http://127.0.0.1:8080
```

Open that address for a page with every instance, its phase and join id, start, stop, backup and
restore controls, and a live log. The same data is available as JSON and server-sent events:

| Endpoint | Behavior |
| --- | --- |
| `GET /api/instances` | every instance with its resolved phase |
| `GET /api/instances/{name}` | one instance with its resolved phase |
| `POST /api/instances/{name}/start` | start in the background, return once it runs |
| `POST /api/instances/{name}/stop` | stop with the usual grace period |
| `GET /api/instances/{name}/logs?tail=N` | log stream as server-sent events (default 100 lines) |
| `GET /api/instances/{name}/backups` | archives in `backups/`, newest first |
| `POST /api/instances/{name}/backup` | archive `data/`, applying the default retention of 5 |
| `POST /api/instances/{name}/restore` | restore one archive named in the JSON body, stopped only |

```bash
curl -s http://127.0.0.1:8080/api/instances
curl -N http://127.0.0.1:8080/api/instances/corekeeper/logs?tail=50
```

The panel binds to loopback and has no authentication. Use `--addr` only behind a reverse proxy
that adds its own access control.

## How it works

```mermaid
graph TD
    A["cmd/gamehost main"] --> B["internal/app"]
    B --> C["internal/instance"]
    B --> J["internal/games"]
    J --> D["internal/corekeeper"]
    J --> K["internal/projectzomboid"]
    B --> E["internal/steamcmd"]
    B --> F["internal/doctor"]
    B --> I["internal/httpapi"]
    C --> G["internal/store"]
    D --> H["internal/process"]
    E --> H
    I --> G
    I --> H
```

| Package | Responsibility |
| --- | --- |
| `internal/app` | argument parsing, command dispatch, exit codes |
| `internal/instance` | domain model, defaults and invariants |
| `internal/store` | instance configuration and runtime state |
| `internal/atomicfile` | replace a file atomically, never leaving a partial copy |
| `internal/process` | process group, signals, grace period, exit codes |
| `internal/logs` | newest-log lookup, tail ring buffer, follow loop |
| `internal/game` | driver contracts: install args, launch, join, session id |
| `internal/games` | game name to driver registry |
| `internal/corekeeper` | Core Keeper launch arguments and join information |
| `internal/projectzomboid` | Project Zomboid install, launch and join information |
| `internal/steamcmd` | locating and running SteamCMD |
| `internal/doctor` | environment probes |
| `internal/httpapi` | JSON API, SSE log stream and embedded panel |
| `internal/home` | data directory resolution |

Server lifecycle:

```mermaid
stateDiagram-v2
    [*] --> stopped
    stopped --> starting: start --detach
    starting --> running: server process launched
    starting --> crashed: launch failed
    stopped --> running: start
    running --> stopped: Ctrl+C, stop, or clean exit
    running --> crashed: unexpected exit
    crashed --> running: start again
```

`start` writes `state.json` before the process is reported as running, and again after it exits, so
`status` never has to guess. If a process disappears while `state.json` still says `running`,
`status` reports `crashed`.

Detached runs re-execute the same binary as a hidden `__supervise` command in a new session. The
supervisor owns the server process, records every phase change and outlives the terminal that
started it. `stop` signals the server's process group; the supervisor observes the exit and writes
the final state.

`serve` is also the backup daemon: it re-reads the instances every 30 seconds, finds the ones whose
window elapsed and drives stop, archive, prune and start through the same helpers the CLI uses.

## Data layout

Everything mutable lives under `GAMEHOST_HOME` (default `~/.local/share/gamehost`):

```
servers/<name>/
├── instance.json     configuration
├── state.json        phase, pid, start time, last exit code
├── files/            SteamCMD install directory (the game server itself)
├── data/             -datapath: worlds/, ServerConfig.json, mods/, backups
├── logs/             one log file per run, newest ten kept
├── backups/          <timestamp>.tar.gz, pre-restore_<timestamp>.tar.gz
└── run.lock          held by the running server or a data command (flock)
```

The repository never contains generated data.

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| `doctor` reports `steamcmd not found` | SteamCMD is not on `PATH` | install the official tarball under `~/.local/share/steamcmd` or set `GAMEHOST_STEAMCMD` |
| `start` opens a window | the server initializes graphics even with `-batchmode` | keep the desktop session, or run it under Xvfb |
| `server is not installed` | `install` was never run | `gamehost install <name>` |
| SteamCMD exits 0 but nothing installed | SteamCMD reports failures in its output | `install` verifies the binary on disk and fails when it is missing |
| Assert dump after `stop` during startup | the world is still generating | wait for `Started session` in `gamehost logs -f` before stopping |
| Friends cannot join a Project Zomboid server | ports `16261` and `16262` stay UDP | forward both, and allow them in the firewall |
| `start-server.sh: Permission denied` | SteamCMD kept the script without the execute bit | `chmod +x files/start-server.sh` |
| `backup`, `restore` or `update` refuses to run | the server holds the world files | `gamehost stop <name>` first |
| `instance is already running` or `instance is in use by another gamehost command` | the server, or a command between its check and its state write, holds `run.lock` | wait for it, or `gamehost stop <name>` if a server was left behind |
| A scheduled backup never runs | `serve` is not running, or the interval has not elapsed since the newest archive | keep `gamehost serve` running and watch its output |
| The server restarted by itself | a scheduled backup window stopped it to archive a consistent world | raise or disable `--backup-every` |
| `text file busy` in tests | `fork` inherits open write descriptors | do not run write-and-exec tests in parallel |

## Development

```bash
make build   # bin/gamehost
make test    # go test -race ./...
make lint    # go vet ./...
make fmt     # gofmt -l -w .
```

The full design, the verified Core Keeper launch contract and the decision log live in
[docs/design.md](docs/design.md). Working conventions for agents and contributors live in
[AGENTS.md](AGENTS.md).

## Design decisions

- **Native process over containers.** A container would outsource lifecycle, signals and I/O to the
  engine, which is exactly the part worth learning. A Podman backend can be added later behind the
  same `process` interface.
- **JSON configuration, no dependencies.** The standard library covers it and the CLI is the
  primary editor.
- **Foreground before daemon.** A visible process and a plain log file are easier to debug and test
  than a detached supervisor; `--detach` comes after the core is proven.
- **One root directory.** All mutable state under `GAMEHOST_HOME`, so the repository stays clean and
  backups are a single path.
- **Two games behind small interfaces.** `internal/game` holds `Installer` and `Runner`, and
  `internal/games` resolves a game name to its driver; the CLI and the API never switch on the game
  name to build a command line.
- **Configuration only edits what a flag can express.** `config` rewrites `instance.json` and leaves
  `ServerConfig.json`, the ini and the game ID file to the server, so it cannot invent a schema it
  does not own. The world name and seed stay immutable because the save already exists.
- **A schedule is a maintenance window.** Backups are only consistent with the world closed, so a
  due backup stops the server, archives, prunes and starts it again; `serve` owns that loop because
  it is the only long-lived process. The same process starts instances on boot with `--start`, so
  a reboot needs one unit instead of two owners of the same process.
- **The admin list belongs to the server.** Core Keeper rewrites `Admins.json` on exit, so `admin`
  only edits it while stopped, writes it atomically in the server's own shape and renumbers the
  entries; the manager never keeps a second copy.
- **The panel only accepts same-origin writes.** A state-changing request must carry an `Origin`
  that matches the host that served the page, or no browser headers at all like a CLI client, and
  never `Sec-Fetch-Site: cross-site`. Reads stay open: a stray link cannot stop a world.

## License

MIT. See [LICENSE](LICENSE).
