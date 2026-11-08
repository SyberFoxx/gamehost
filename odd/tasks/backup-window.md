# Backup window inside the supervisor

Objective: the scheduled backup actually runs for every way a server is started (TUI, `start --detach`,
systemd), stops the server for a clean archive, prunes each archive class, and writes every step to the
run log. Branch `feat/backup-window`. Test runner: `go test -race ./...`.

## Specs

S1. A running instance backs itself up on its own schedule without any extra process or daemon.

S2. The window runs while the instance's supervisor is alive: stop the server, archive `data/`, prune,
   start it again. The same supervisor restarts it and keeps the same log file.

S3. A failed archive still restarts the server. A stop requested by the user during the window wins:
   the server stays down.

S4. Every step is written to the run log the supervisor already owns: the stop, the archive path and
   size, each removed archive, and the restart. A scheduled backup must be visible in `gamehost logs`
   and the TUI Logs screen.

S5. Retention is per class: timestamped archives keep `keep` newest, and `pre-*` safety archives
   (`pre-update`, `pre-restore`, `pre-import`) keep `keep` newest too. A safety archive is never the
   first thing deleted.

S6. `gamehost serve` and the TUI Web panel no longer run a scheduler of their own: one scheduler per
   instance, owned by its supervisor.

S7. `service install` no longer writes a backup unit or timer, and it removes stale ones. The main
   unit runs `gamehost start` in the foreground, so the supervisor schedules there too.

S8. The TUI Backups screen shows the schedule and the newest archive, and `e` edits the interval and
   retention without stopping the server. `gamehost config --backup-every/--backup-keep` keeps working,
   including while the server runs (it takes effect on the next tick).

S9. The schedule is off by default. An empty interval removes the schedule and its keep.

## Tasks

| ID | Specs | Route | Status | Commit |
| --- | --- | --- | --- | --- |
| T1 | S5 | inline | done | 93068be |
| T2 | S1-S4 | inline | done | 93068be |
| T3 | S6 | inline | done | 93068be |
| T4 | S7 | inline | done | 93068be |
| T5 | S8-S9 | inline | done | (next) |
| T6 | docs | inline | done | (next) |

## Log

L1 (2026-11-08, verbatim): "por cierto con respecto a corekeeper y el gamehost, funciono bien todo dure
un buen rato jugando con un amigo pero... no vi los logs que hiciera algun backup y por cierto esos
backups que no se almacenen como loco, por cierto, que haga auto guardado solo del mundo no se puede?,
seria bueno, aunque se que el juego lo hace ve si el server se le puede poner o configurar esos asi como
un apartado ed configuracion de los backups en el TUI"

L2: the user's Core Keeper instance has `"backup": {}` (off), and the only scheduler lived in `serve`
and in the Web panel screen, so a TUI `start --detach` never scheduled anything. The panel also passed
`io.Discard`, so nothing was visible.

L3 (2026-11-08): `backup.Prune` counted `*.tar.gz` together, and `pre-*` sorts after a timestamp, so a
prune deleted the safety archives first.

L4 (2026-11-08): verified live that Project Zomboid preserves `SaveWorldEveryMinutes=15` across a start
(see `memory-at-setup.md`).

L5 (2026-11-08): T5. `Item.BackupEvery`, `ServerBackend.Configure` and the Backups screen `e` form. The
summary reads the newest archive from the loaded list, so a fresh backup updates it without a second
backend call. `lobbyConfigure` saves through `applySettings` without taking `run.lock`, because the
supervisor holds it for the whole run.

L6 (2026-11-08): T6. `docs/design.md` M18, the systemd section, the config decision and the schedule
decision; `docs/manual.md` backups section; `AGENTS.md` traps.
