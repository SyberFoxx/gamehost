# Project Zomboid: console channel, owned settings, admins and mods

Feature branch `feat/pz-console`, delivery `single-pr`, runner `go test -race ./...`.

## Specs

- **S1** — A Project Zomboid launch reads console commands from a control file gamehost owns, so
  `save` and `quit` work the way the PZ devs document (`echo save > <fifo>`,
  `pzwiki.net/wiki/Dedicated_server#Systemd`), instead of the `SIGTERM` they discourage.
- **S2** — `gamehost stop` sends the driver's clean-stop commands first and only escalates to
  `SIGTERM`/`SIGKILL` when the server does not leave within the console grace period. A game with no
  console keeps the exact behaviour of today.
- **S3** — The same clean stop applies to a cancelled foreground run (Ctrl+C) and to the scheduled
  backup window; a hung server still ends, because the signals stay as the fallback.
- **S4** — `maxplayers` and the join `password` become settable for Project Zomboid, written into
  `Server/<name>.ini` as `MaxPlayers=` and `Password=` before every launch, in place, with every
  other byte preserved.
- **S5** — `game.AdminStore` for Project Zomboid: `admin add|remove|list` and the Admins screen work
  through the console (`adduser`, `setaccesslevel`, `removeuserfromwhitelist`) and the server
  database, never a hand-edited file.
- **S6** — `mods add --workshop <id>` resolves the mod ids from the downloaded workshop content, so
  one workshop id no longer needs a second command with a second id.
- **S7** — The mods screen reports `require=`/`incompatible=` problems before a restart and can
  reorder `Mods=`, because map mods load top-down.
- **S8** — Every launch flag, path, state change and command in these specs updates `docs/design.md`
  and the `AGENTS.md` traps in the same commit.

## Tasks

| ID | Specs | Route | Status |
| --- | --- | --- | --- |
| T1 | S1 | inline | done — `internal/process` control file |
| T2 | S1 | inline | done — `game.Console` + `projectzomboid.Runner` |
| T3 | S1, S3 | inline | done — launch stdin, stop hook, window |
| T4 | S2 | inline | done — `stopInstance` console path |
| T5 | S8 | inline | done — design.md, AGENTS.md, README, M19 |
| T6 | S4 | inline | pending — `MaxPlayers=` and `Password=` in the ini |
| T7 | S5 | inline | pending — `game.AdminStore` through the console |
| T8 | S6, S7 | inline | pending — workshop id resolution and mod order |

## Log

- **L1** (user): "revisa en mi home una carpeta llamada gamehost, quiero actualizarle una cosita, y
  es, ver que botones falta por programar en mi TUI, revisa alli para ver que nos falta, y luego
  tocar la estructura de proyecto zomboid que haremos para que le gestor puedo gestionar servers de
  la forma mas excelente de PZ hasta aguatando con mods"
- **L2** (user): "ok vamos con ese plan, si revisas mi home, alli tengo una carpeta llamada skills y
  detro de otra garpeta llamda go-skill hay un archivo con mis skills del lenguaje Go, trabajemos lo
  que me PZ que me dices entonces, cada cambio que hagas subo a github por favor"
- **L3** (audit): CLI/TUI parity is complete (`odd/tasks/tui-complete.md`, T1–T15). The three real
  holes are PZ-only: no `game.AdminStore`, `MaxPlayers`/`Password` rejected in
  `validateProjectZomboid`, and mods that need two manual ids with no order or dependency check.
- **L4** (evidence): `stop` today signals the process group (`internal/process/process.go:135`).
  PZwiki documents shutdown as `echo save > control; sleep 15; echo quit > control` and says the devs
  explicitly discourage the systemd unit that signals instead.
- **L5** (audit): T1–T5 landed as milestone M19. `process.OpenControl` creates and holds the fifo,
  `game.Console` reaches the driver without a `switch`, the launch hands it as stdin, and `stop`,
  Ctrl+C and the backup window ask for `save`/`quit` before the 60 s signal grace. Verified with
  `gofmt`, `go vet` and `go test -race ./...`. The three specs that were never started stay open as
  T6–T8: S4 (`MaxPlayers`/`Password`), S5 (PZ admin store through the console) and S6/S7 (workshop
  id resolution, mod order and dependency checks).
