# gamehost — design

A terminal-first manager for dedicated game servers, written in Go, starting with **Core Keeper**.

The project doubles as a learning vehicle: every milestone introduces a Go concept that a real
program needs (process control, signals, concurrency, `net/http`, tests with `-race`), instead of
synthetic exercises.

**Status:** M0–M5 implemented (`doctor`, instance model and store, `install`/`update`,
`start`/`status` with join information, `start --detach`, `stop`, `logs`/`logs -f`,
`backup`/`restore`). M6 is next.

## 1. Goals and non-goals

**Goals (MVP)**

- Install and update a Core Keeper dedicated server with SteamCMD.
- Create and manage server instances (world name, seed, mode, player limit, network mode).
- Start, stop and inspect a server process reliably, with graceful shutdown.
- Stream and follow the server log.
- Back up and restore world data.
- Keep the code idiomatic: small packages, explicit errors, no hidden global state.

**Non-goals (for now)**

- Docker or Podman orchestration.
- Mod management (mod.io).
- Discord webhooks, metrics, scheduled jobs.
- A second game driver, TUI or web UI before the Core Keeper CLI is solid.

## 2. Verified facts about the Core Keeper dedicated server

Primary source: the `ARGUMENTS.txt` and launch scripts shipped **inside the game files**
(`CoreKeeperServer` install directory), which are authoritative. Secondary source:
[`escapingnetwork/core-keeper-dedicated`](https://github.com/escapingnetwork/core-keeper-dedicated),
the widely used container image.

| Fact | Value |
| --- | --- |
| Dedicated server Steam app | `1963720` |
| Companion app installed by the image | `1007` (Steam client libraries, loaded from `steamcmd/linux64`) |
| Steam login | `anonymous` |
| Binary | `CoreKeeperServer` in the install directory, run with that directory as cwd |
| Join credentials | `GameInfo.txt` next to the binary, containing `GameID: <id>`; `GameID.txt` also appears |
| Session log line | `Started session with info: <id>` |
| World data | directory passed with `-datapath`; without it, a `DedicatedServer` subfolder of the normal save location |
| Files inside `-datapath` | `ServerConfig.json`, `Admins.json`, `mods/`, `modloader/`, world saves |
| Player count | default `8`; the shipped guide warns that raising it may make sessions unstable |

Launch flags:

```
-batchmode -logfile <path> -world <0-29> -worldname <name> -worldseed <seed>
-worldmode <Normal|Hard|Creative|Casual> -hashedworldseed <uint32> -gameid <id>
-datapath <dir> -maxplayers <n> -season <name> -ip <addr> -port <port>
-password <pw> -allowonlyplatform <Steam|Epic|Microsoft|GOG>
-activatecontent <list> -activateallcontent
```

Corrections found by reading the shipped guide instead of the container recipe:

- `-worldmode` takes **names**, not numbers. `ServerConfig.json` stores numbers; the flag does not.
- Game IDs are alphanumeric but **must not contain `Y`, `y`, `x`, `0` or `O`**, 15–28 characters.
- `-ip` has no effect without `-port`, and `-allowonlyplatform` only applies to direct connections.
- `-logfile /dev/stdout` works: the server writes everything to stdout, so the manager can tee it.

Network modes:

- **SDR** (no `-port`): traffic goes through Steam relay, no port forwarding. Default for a small
  friend group.
- **Direct connect** (`-port` set): lower latency, requires opening and forwarding UDP ports.

Validated by running it on this machine:

- The binary starts with `-batchmode` and no `-nographics`; it initializes graphics and opens a
  window on the desktop display. The official script runs it under Xvfb, so a display is expected.
- No missing shared libraries: the launch needs neither app `1007` nor a custom `LD_LIBRARY_PATH`
  when the desktop session provides the graphics stack.

Still open:

1. Whether running under Xvfb is worth it to hide the window on a desktop host.
2. Exact save-file layout inside `-datapath` once a world finishes generating.

## 3. Architecture

```mermaid
graph TD
    A["cmd/gamehost main"] --> B["internal/app"]
    B --> C["internal/instance"]
    B --> D["internal/corekeeper"]
    B --> E["internal/steamcmd"]
    B --> F["internal/backup"]
    B --> G["internal/httpapi (M6)"]
    C --> H["internal/store"]
    D --> I["internal/process"]
    E --> I
    I --> J["internal/logs"]
    G --> I
```

| Package | Responsibility | Introduced |
| --- | --- | --- |
| `internal/app` | parse args, dispatch commands, own exit codes | M0 |
| `internal/store` | load/save instance config and runtime state (JSON, atomic write) | M2 |
| `internal/instance` | domain model and validation | M2 |
| `internal/process` | supervise one OS process: start, signal, wait, state | M3 |
| `internal/logs` | log file plus in-memory ring buffer and subscribers | M4 |
| `internal/corekeeper` | translate an instance into `CoreKeeperServer` args, read `GameID.txt` | M3 |
| `internal/steamcmd` | install/update app `1963720` and validate the binary | M1 |
| `internal/backup` | tar.gz snapshots of the data directory with retention | M5 |
| `internal/httpapi` | HTTP API plus SSE log stream and embedded UI | M6 |

Rules that keep the project idiomatic:

- Interfaces are declared where they are consumed and stay at 1–3 methods.
- Constructors take dependencies, return concrete types.
- `context.Context` first parameter on anything that blocks or does I/O.
- Errors are wrapped with `%w` and the operation that failed.
- No `init`, no mutable package-level state; the config root is injected.

## 4. Domain model

```go
type Instance struct {
    Name       string
    Game       string // "corekeeper"
    InstallDir string
    DataDir    string
    World      World
    Network    Network
    MaxPlayers int
    GameID     string
}

type World struct {
    Index int
    Name  string
    Seed  string
    Mode  string // normal | hard | creative | casual
}

type Network struct {
    Mode     string // sdr | direct
    Port     int
    Password string
}
```

Runtime state is separate from configuration, so a crashed server never corrupts the config:

```go
type State struct {
    Phase     Phase     // stopped | starting | running | stopping | crashed
    PID       int
    StartedAt time.Time
    ExitCode  int
    GameID    string
}
```

```mermaid
stateDiagram-v2
    [*] --> stopped
    stopped --> starting: start --detach
    stopped --> running: start
    starting --> running: supervisor launched the server
    starting --> crashed: launch failed
    running --> stopping: stop or SIGINT/SIGTERM
    stopping --> stopped: exit within grace period
    stopping --> crashed: killed after timeout
    running --> crashed: unexpected exit
    crashed --> starting: start
```

`stopping` is not persisted: the supervisor is inside the grace period, and `status` still reports
`running` because the process is alive. The next write is the terminal phase.

## 5. Runtime layout

Single root, overridable with `GAMEHOST_HOME`:

```
$XDG_DATA_HOME/gamehost/            default ~/.local/share/gamehost
└── servers/<name>/
    ├── instance.json               user configuration
    ├── state.json                  runtime state, rewritten on every transition
    ├── files/                      SteamCMD install dir (-force_install_dir)
    ├── data/                       -datapath, holds worlds
    ├── logs/<timestamp>.log        one file per run
    └── backups/                    <timestamp>.tar.gz, pre-restore_<timestamp>.tar.gz
```

Nothing generated by the manager lives inside the git repository.

## 6. Command line

| Command | Behavior | Milestone |
| --- | --- | --- |
| `gamehost version` | print build version | M0 |
| `gamehost doctor` | check SteamCMD, write permissions, disk space, display | M0 |
| `gamehost install <name>` | create instance if needed and download app `1963720` | M1 |
| `gamehost update <name>` | SteamCMD `app_update ... validate` | M1 |
| `gamehost create <name>` | write `instance.json` from flags | M2 |
| `gamehost list` | table of instances and phases | M2 |
| `gamehost show <name>` | config plus last known state | M2 |
| `gamehost start <name>` | run in foreground, log to stdout and file; Ctrl+C stops | M3 |
| `gamehost start <name> --detach` | re-exec a supervisor child, log to file only | M3b |
| `gamehost status <name>` | phase, pid, uptime, game ID | M3 |
| `gamehost stop <name>` | SIGTERM, wait grace period, SIGKILL | M3b |
| `gamehost logs <name> [-f]` | tail or follow the current log | M4 |
| `gamehost backup <name>` | snapshot `data/`, apply retention | M5 |
| `gamehost restore <name> <archive>` | restore after confirmation | M5 |
| `gamehost serve [--addr]` | HTTP API and UI | M6 |

`start` runs in the foreground first on purpose: one process, one log stream, no PID guessing.
The detached supervisor was a separate milestone because it is the hardest concurrency problem in
the project and deserved its own design and tests.

### Detached supervision

`start --detach` re-executes the same binary as a hidden `__supervise` command:

```
gamehost start X --detach
  └─ gamehost __supervise X --log <path>   (new session, stdout = log file)
       └─ CoreKeeperServer                 (new process group, stdout = supervisor stdout)
```

Why this shape:

- **One implementation of the lifecycle.** The foreground run and the supervisor call the same
  `serve` function, so signals, exit codes and state writes cannot drift apart.
- **A new session, not a double fork.** `Setsid` removes the controlling terminal, so closing the
  terminal does not send `SIGHUP`, and the child survives its parent without PID files.
- **The supervisor stays alive while the server runs.** It is the only writer of `state.json` during
  the run, which keeps phases consistent and lets `status` detect crashes.
- **`stop` signals the server's process group**, not the supervisor. The supervisor observes the exit
  and records it, so a stop looks exactly like a clean shutdown.
- **A signal from outside counts as a clean stop.** If the server dies from `SIGTERM` or `SIGINT`,
  `process.Result.Stopped` is true; otherwise an external `stop` would be reported as a crash.
- **A zombie is not a running server.** `Alive` reads `/proc/<pid>/stat` and treats state `Z` as dead,
  so `stop` does not wait the full grace period for a process that is already gone.

Tradeoffs accepted:

- The supervisor is a hidden command in the public CLI surface. It is undocumented in `usage` and
  refuses anything but an instance name.
- `--detach` needs a real binary: `go run` produces a temporary executable that disappears, so
  detaching from `go run` is not supported.
- PID reuse can fool the liveness check. The window is small and the consequence is refusing to
  start or stopping an unrelated process; a stronger scheme would store the process start time.

## 7. Milestones

| # | Deliverable | Acceptance | Go concepts |
| --- | --- | --- | --- |
| M0 | skeleton, `version`, `doctor` | `go build`, `go test -race`, `doctor` reports missing SteamCMD on this machine | packages, `io.Writer` injection, error wrapping, table tests |
| M1 | `install`, `update` | server binary and Steam libs present, update is idempotent | `os/exec`, pipes, context cancellation, exit codes |
| M2 | `create`, `list`, `show` | validation rejects bad names, ports and player counts; config survives restart | structs, JSON, atomic writes, `t.TempDir` |
| M3 | `start`, `status` | foreground run, SIGINT shutdown, crashed state detected after `kill -9`, join id reported | process groups, signals, `exec.Cmd`, timeouts |
| M3b | `--detach`, `stop` | `start` returns, `status` sees the server, `stop` saves and exits, no orphans | re-exec, `Setsid`, process groups, `/proc` |
| M4 | `logs` | `-f` follows live output without losing lines | `bufio.Scanner`, ring buffer, `ReadAt`, ticker |
| M5 | `backup`, `restore` | archive restores into a working world | `archive/tar`, `compress/gzip`, `filepath.WalkDir` |
| M6 | `serve` | start/stop/status from the browser, live log via SSE | `net/http`, `Server.Shutdown`, `embed` |
| M7 | second game driver | `game.Driver` interface with two implementations | interfaces at the consumer, plugin-style layout |

Each milestone is one work unit: code, tests and doc updates in the same commit.

## 8. Testing strategy

- Table-driven tests, `t.Run`, `t.Parallel` where there is no shared state.
- Always `go test -race ./...`.
- Filesystem tests use `t.TempDir()`; no test writes into the repository.
- External processes are replaced by small hand-written fakes behind a 1–3 method interface.
- `steamcmd` and the real server are exercised by a manual checklist, not by the default test
  suite; anything network or multi-gigabyte gets a `//go:build integration` tag.
- The log follower is tested with a mutex-guarded buffer: the writer appends from the test, the
  reader follows in another goroutine, and a bounded wait keeps the test deterministic.
- `doctor` is the first command because it makes the environment explicit and testable.

## 9. Risks

| Risk | Mitigation |
| --- | --- |
| SteamCMD not installed on Nobara (checked: absent) | `doctor` explains the two install paths; M1 documents the working one |
| Native server needs libraries from app `1007` | validate `LD_LIBRARY_PATH` in M1 before writing the supervisor |
| Server needs a display despite `-batchmode` | M3 smoke test; fall back to Xvfb only if proven necessary |
| Hosting and playing on the same machine | measure RAM/CPU during a 5-player session; `status` reports resource hints later |
| Direct-connect mode requires router changes | default to SDR (no `-port`) |
| Save corruption from hard kills | graceful stop with timeout, backup before update |

## 10. Decisions

- **Native process over containers.** Podman would outsource lifecycle, signals and I/O to the
  engine, which is exactly the part worth learning in Go. A container backend can be added later
  behind the same `process` interface.
- **JSON config, no dependency.** The standard library covers it; the CLI is the primary editor.
  TOML would add a dependency before it earns its place.
- **Foreground before daemon.** A visible process and a plain log file are easier to debug and to
  test than a detached supervisor, so `start` shipped first.
- **Re-exec instead of a double fork.** One lifecycle implementation, called by both the foreground
  command and the detached supervisor. A double fork would duplicate the supervisor logic in a
  second binary or a second code path.
- **Backups need a stopped server.** Core Keeper writes the world in place; archiving a live
  directory can capture a half-written save. Refusing while the server runs is simpler and safer
  than pretending a live tar is consistent.
- **Restore keeps the previous data.** The archive is extracted into a staging directory, the
  current `data/` is moved aside, and only then the staging directory takes its name. A failed
  extraction never touches the world, and a bad archive can be undone with the pre-restore copy.
- **Archive entries that escape the target are rejected.** A tar can name `../x` or an absolute
  path; the extractor refuses anything that is not local to the restore directory.
- **One root directory.** All mutable state under `GAMEHOST_HOME`, so the repository stays clean
  and backups are a single path.
