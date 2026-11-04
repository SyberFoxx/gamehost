# AGENTS.md

## Context

`gamehost` is a terminal-first manager for dedicated game servers, written in Go 1.26.
First supported game: Core Keeper (Steam app `1963720`).
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
- Verify Core Keeper behavior against the documented contract; do not invent flags.
  Reference image: `github.com/escapingnetwork/core-keeper-dedicated` (`scripts/compile-parameters.sh`).
- Changes to launch flags, paths or the state machine must update `docs/design.md` in the same change.
- One milestone per work unit: code, tests and docs together.

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
- The restore extractor rejects any tar entry that is not local to the target directory; keep that
  check when touching `internal/backup`.
- World data lives under `-datapath`, not next to the binary. Back up that directory.
- Setting `-port` switches the server from Steam relay (SDR) to direct connect and requires
  port forwarding; leave it unset for a small friend group.
