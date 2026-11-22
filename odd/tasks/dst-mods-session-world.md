# Don't Starve Together: mods, session id and world lifecycle

Feature branch `feat/dst-mods-session-world`, delivery `single-pr`, runner `go test -race ./...`.
Test-first: each rule gets a failing test before its implementation.

## Specs

- **S1** — "agregemos eso de mods a DST": the Don't Starve Together driver implements
  `game.ModStore`, so `mods list|add|remove` and the lobby Mods screen work for it. A workshop id is
  installed with `ServerModSetup("<id>")` in `<install>/mods/dedicated_server_mods_setup.lua`; the
  corresponding `workshop-<id>` is enabled in `<cluster>/<shard>/modoverrides.lua` for Master and,
  when present, Caves. Other entries and player configuration in those files are preserved.
  Source: Don't Starve Wiki, "Don't Starve Together Dedicated Servers", sections Modding and
  Command Line Options (`-ugc_directory`, `-skip_update_server_mods`, `-only_update_server_mods`).
- **S2** — "el apagado por señal si me dices que es la mejor forma": keep the signal stop for DST.
  Evidence: the verified real run stopped on `SIGTERM` with exit 0 (`docs/design.md`), the DST wiki
  Linux service script stops with `kill`, and the documented `-console` flag feeds only the primary
  process while Caves is an `Extra` process with no stdin. No code change; record the decision.
- **S3** — "session ID que no vuelva vacio": `SessionID` and `Join` report the identifier the server
  logs as `Telling Client our new session identifier: <hex>` in `<cluster>/Master/server_log.txt`,
  read from the most recent matching line. Before the first start, `Join` keeps returning the
  cluster name instead of an empty value. The id is never invented or persisted in `instance.json`.
- **S4** — "con respect oa la reacreacion o rollback de mundo seria bueno": an operator can recreate
  a DST world. The command requires a stopped instance, writes a `pre-recreate_` safety archive of
  the data directory, then removes `<cluster>/Master/save/` and `<cluster>/Caves/save/` so the next
  start generates new worlds from the existing `worldgenoverride.lua`/`leveldataoverride.lua`. It
  asks for confirmation before acting and never deletes the cluster's ini, token or admin files.
- **S5** — World rollback restores a named gamehost backup through the existing `restore` path,
  because the on-disk layout of DST's internal save snapshots is not documented. `world rollback`
  is a named entry point to that verified path with the same stopped check and safety archive; it
  does not claim to call the game's `c_rollback` console command.
- **S6** — Every new file, flag, path, command and state change updates `docs/design.md` and the
  `AGENTS.md` traps in the same work unit.

## Tasks

- T1 | S1 | inline | done in `7c2921b` — DST `ModStore` + tests
- T2 | S3 | inline | done — session id parser, `SessionID`, `Join` + tests
- T3 | S2, S4, S5 | inline | done — signal decision, world lifecycle command + tests
- T4 | S6 | inline | pending — design.md, AGENTS.md, README/CLI help
- T5 | S1-S6 | inline | pending — gofmt, vet, race suite, push

## Log

- **L1** (user, verbatim): "ok, agregemos eso de mods a DST y el apagado por señal si me dices que es la mejor forma, session ID que no vuelva vacio y con respect oa la reacreacion o rollback de mundo seria bueno"
- **L2** (verification, 2026-11-22): Klei's command-line guide documents `-persistent_storage_root`,
  `-conf_dir`, `-cluster`, `-shard`, `-offline`, `-port`, `-players`, `-token`, `-ugc_directory`,
  `-console`, `-skip_update_server_mods`, `-only_update_server_mods` and
  `-monitor_parent_process <pid>`; it documents no shutdown flag. The DST wiki documents
  `dedicated_server_mods_setup.lua` with `ServerModSetup("id")` and
  `ServerModCollectionSetup("id")`, per-shard `modoverrides.lua` returning
  `{ ["workshop-id"] = { enabled = true } }`, and `c_rollback()` / `c_regenerateworld()` as
  in-game console commands that need a backup first.
- **L3** (decision): signals stay for DST. `SIGTERM` already stopped the verified cluster cleanly,
  the wiki's Linux service example stops with `kill`, and `-console` cannot reach the Caves shard.
  Adding a Lua console channel would risk its stdin plumbing and leave Caves unsaved.
- **L4** (scope): `ServerModCollectionSetup` and manual non-Workshop mods remain editable by hand;
  the CLI manages one Workshop id at a time, matching the existing `mods add|remove` shape.
- **L5** (T2 presentation decision): the status, lobby and HTTP readers prefer `Join()` over
  `state.GameID`, so returning the cluster name from `Join()` forever would hide the session id.
  DST now returns `("session id", <hex>)` once the log has one and `("cluster", <name>)` before the
  first start. The cluster name remains in `instance.json`; the session id stays only in the log and
  the runtime state, never in config.
- **L6** (T3 confirmation): the CLI has no stdin channel and no existing prompt helper, so
  `world recreate` requires an explicit `--yes` instead of asking interactively. `restore` and
  `import-world` rely on their safety archive alone; recreating a map is irreversible in-game, which
  justifies the extra gate. The lobby is not wired to `world`; its Backups screen already calls the
  same restore helper, and docs record the commands as command-only.

Local locator: odd/tasks/dst-mods-session-world.md
