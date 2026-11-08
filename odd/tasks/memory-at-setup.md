# Memory at setup and world autosave

Objective: Project Zomboid always stores an explicit heap, the setting is changeable while the server
runs (the server restarts for it), and the world autosaves on an interval the manager applies. Branch
`feat/backup-window`. Test runner: `go test -race ./...`.

## Specs

S1. `instance.New` stores an explicit heap for Project Zomboid: an empty `Memory` becomes
   `DefaultProjectZomboidMemory` (`8g`). `instance.json` never carries a missing heap for PZ.

S2. The create wizard prefills `8g` in the Memory field and still shows it only for Project Zomboid.

S3. `gamehost config <name> --memory 4g` on a running Project Zomboid server validates the value,
   stops the server, saves the instance and starts it again. The new heap is applied by the restart.
   A value that cannot boot (below `512m`, above `MemTotal`) is refused before the stop, and the
   running server keeps running.

S4. The TUI Settings form follows the same rule: a memory-only change restarts the server; any other
   change while running still asks for a stopped server.

S5. `--autosave MINUTES` on `create` and `config` for Project Zomboid only, stored in `instance.json`,
   `0` (default) leaves the game's own default. The driver writes `SaveWorldEveryMinutes=<n>` into
   `<data>/Server/<world>.ini` before every launch, replacing only that line and keeping every other
   byte. A missing ini (first start) skips the write; the value applies on the next start.
   The TUI shows an Autosave field for Project Zomboid.

S6. Core Keeper and Don't Starve Together reject `--autosave` and reject the memory field, like the
   other game-specific settings. Core Keeper and DST save on their own; gamehost exposes no interval
   for them.

S7. `gamehost show` and a flagless `config` print the heap and the autosave interval.

## Tasks

| ID | Specs | Route | Status | Commit |
| --- | --- | --- | --- | --- |
| T1 | S1, S6 | inline | done | (next) |
| T2 | S5 | inline | done | (next) |
| T3 | S3, S7 | inline | done | (next) |
| T4 | S2, S4, S5 | inline | done | (next) |
| T5 | docs | inline | done | (next) |

## Log

L1 (2026-11-08, verbatim): "ajusta todo eso que cuando se vaya a montar el server se esconja siempre la
RAM y si el usuario quiere cambiarlo pues qu entre en un apartado y lo cambie como todo, el server
deberia reiniciarse para eso"

L2 (2026-11-08): the shipped PZ heap is `8g`, so an empty setting only means "the value the game ships
with". Storing it explicitly removes the ambiguity and makes `show` honest.

L3 (2026-11-08): the live `zomboid` server kept `SaveWorldEveryMinutes=15` after a stop and a start, so
the game reads the ini value and does not reset it. The setting is written before every launch through
`game.Preparer`, exactly like the heap, so a manual edit or a restored ini cannot silently disable it.

L4 (2026-11-08): the PZ ini only exists after the first start, so the first launch of a brand new
instance cannot carry the interval. It applies from the second start; a `config --autosave` restart
applies it immediately.

L5 (2026-11-08): T1. `instance.New` fills `8g` for Project Zomboid, and both other games reject
`AutosaveMinutes` like they reject `Memory`.

L6 (2026-11-08): T2. `SaveWorldEveryMinutes` is written by the driver's `prepare`, after the heap,
through the same `setIniValue` the mod store uses. A missing ini is skipped. `AutosaveMinutes` is
range-checked in the model (0-65535).

L7 (2026-11-08): T3. `game.Validator` is the pure check `config` uses before a stop, so the launch
file writes stay in `Prepare`. `restartInstance` stops, saves and starts detached; a memory-only
change does not need `run.lock` because the supervisor releases it when the stop completes.

L8 (2026-11-08): T4. The wizard prefills `8g`; the Autosave field is PZ-only. `lobbyUpdate` mirrors
`runConfig`: launch flags need a stop, the schedule is live, heap/autosave restart a running server
after validation.
