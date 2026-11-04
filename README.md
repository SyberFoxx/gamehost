<!-- TODO(banner): uncomment and add the image at docs/assets/banner.png
<p align="center">
  <img src="docs/assets/banner.png" alt="gamehost" width="100%">
</p>
-->

# gamehost

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
| M5 | `backup` / `restore` | next |
| M6 | `serve`: HTTP API and web panel | planned |
| M7 | second game driver | planned |

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

`start` without flags keeps the server in the foreground with its output on the terminal; Ctrl+C
stops it. `start --detach` hands the server to a supervisor in its own session and returns, so the
server survives closing the terminal. `stop` signals the whole process group, waits up to 20
seconds for a clean save, then escalates to `SIGKILL`.

Without `--port` the server uses the Steam relay network, so no router configuration is needed.
Passing `--port 27015` switches it to direct connections, which requires opening UDP ports.

## Commands

| Command | Purpose |
| --- | --- |
| `gamehost doctor` | check platform, data directory, SteamCMD and display |
| `gamehost create <name> [flags]` | create an instance and its directories |
| `gamehost install <name>` | download the server files with SteamCMD |
| `gamehost update <name>` | re-download and validate the server files |
| `gamehost start <name> [--detach]` | run the server, in the foreground or in the background |
| `gamehost stop <name>` | stop a running server, waiting for a clean save |
| `gamehost logs <name> [-n N]` | print the last `N` lines (default 100) of the newest log |
| `gamehost logs <name> -f` | follow the log until Ctrl+C |
| `gamehost status <name>` | report phase, pid, uptime and join information |
| `gamehost list` | list instances |
| `gamehost show <name>` | print the full configuration of one instance |
| `gamehost version` | print the build version |

`create` flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--max-players` | `8` | player limit, 1–8 |
| `--world-name` | `Core Keeper Server` | name shown to connecting players |
| `--seed` | random | world seed, only used on first creation |
| `--mode` | `normal` | `normal`, `hard`, `creative` or `casual` |
| `--port` | `0` | direct-connect port; `0` keeps the Steam relay |
| `--password` | none | direct-connect password, at most 28 characters |
| `--game-id` | generated | fixed game ID, 15–28 characters without `Y y x 0 O` |

Flags work before or after the instance name.

## How it works

```mermaid
graph TD
    A["cmd/gamehost main"] --> B["internal/app"]
    B --> C["internal/instance"]
    B --> D["internal/corekeeper"]
    B --> E["internal/steamcmd"]
    B --> F["internal/doctor"]
    C --> G["internal/store"]
    D --> H["internal/process"]
    E --> H
```

| Package | Responsibility |
| --- | --- |
| `internal/app` | argument parsing, command dispatch, exit codes |
| `internal/instance` | domain model, defaults and invariants |
| `internal/store` | instance configuration, runtime state, atomic writes |
| `internal/process` | process group, signals, grace period, exit codes |
| `internal/logs` | newest-log lookup, tail ring buffer, follow loop |
| `internal/corekeeper` | launch arguments and join information |
| `internal/steamcmd` | locating and running SteamCMD |
| `internal/doctor` | environment probes |
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

## Data layout

Everything mutable lives under `GAMEHOST_HOME` (default `~/.local/share/gamehost`):

```
servers/<name>/
├── instance.json     configuration
├── state.json        phase, pid, start time, last exit code
├── files/            SteamCMD install directory (the game server itself)
├── data/             -datapath: worlds/, ServerConfig.json, mods/, backups
├── logs/             one log file per run
└── backups/          reserved for M5
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

## License

No license has been chosen yet. Until one is added, all rights are reserved.
