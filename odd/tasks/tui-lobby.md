# TUI lobby for gamehost

Interactive lobby rendered by `gamehost` with no arguments on a terminal. Branch `feat/tui-lobby`,
delivery `single-pr` (one new self-contained package plus a TTY gate; no changed existing
behaviour), test runner `go test -race ./...`.

## Specs

- **S1** — `gamehost` with no arguments opens the lobby only when stdout is a terminal. Any other
  stdout (pipe, file, test buffer) keeps printing the current `usage` text and returning
  `ErrUsage`. User words: "al arrancarlo ... no existe un lobby".
- **S2** — The lobby opens with the `GAMEHOST` wordmark in large block letters at the top.
  User words: "me diga timpo GAMEHOST arriba en grande".
- **S3** — A menu the user moves with the arrow keys and confirms with Enter. User words:
  "me muestre los comandos moviendome con flechas yo, es decir las opciones mas didacticas".
  v1 entries: `Start`, `Stop`, `Logs`, `Backup`, `Status`, `Quit`. `q` and `ctrl+c` also quit.
- **S4** — `Start` launches the server detached, exactly like `gamehost start --detach`, and never
  blocks the UI while SteamCMD and the server boot. The lobby shows a spinner and, on success, the
  reported pid.
- **S5** — `Stop` waits for the clean-save window (`stopGrace`) and reports the outcome.
- **S6** — `Logs` shows the last 100 lines of the newest log of the selected instance, scrolls with
  the arrow keys and `pgup`/`pgdn`, refreshes with `r`, and returns with `esc`.
- **S7** — `Backup` archives the world through the existing backup path and reports the archive
  size; a running server is refused with the same message the CLI produces.
- **S8** — `Status` prints the same facts as `gamehost status`: phase, pid, uptime and the join
  information the game driver reports (game ID for Core Keeper, address and port otherwise).
- **S9** — A failed action shows its error inline in the footer, keeps the lobby usable and never
  panics or leaves the terminal in a broken state.
- **S10** — No existing command, flag, output line or exit code changes. `go test -race ./...`
  stays green without modifying the current assertions.
- **S11** — Documentation ships with the code: a TUI section and milestone M12 in `README.md`,
  the package row, command row, milestone row and the dependency decision in `docs/design.md`,
  and the terminal traps in `AGENTS.md`.

## Decisions

- Bubble Tea v2 and Lip Gloss v2, imported as `charm.land/bubbletea/v2` and
  `charm.land/lipgloss/v2`. The upstream module path changed: `github.com/charmbracelet/...` no
  longer resolves for v2.
- The UI owns raw mode, the alternate screen, resize and cleanup; a hand-rolled replacement would
  be roughly 400 lines of ANSI and termios code with no deterministic tests.
- `internal/tui` imports nothing from this project. It receives a `Backend` value whose fields are
  functions, so tests pass literals instead of implementing a six-method fake, and presentation
  types stay independent from the domain model.
- `internal/app` builds the backend from the typed helpers the command layer already exposes
  (`startDetached`, `stopInstance`, `backupInstance`, `games.Lookup().Runner.Join`), so the lobby
  never shells out to the `gamehost` binary and never reimplements a command.
- The TTY gate lives in `app.Run`, not in `main`: `main` stays a two-line dispatcher and the
  current tests keep running against a `bytes.Buffer`.

## Tasks

| ID | Deliverable | Specs | Route | Status |
| --- | --- | --- | --- | --- |
| T1 | dependencies, `internal/tui` skeleton, `Backend`, model state and key handling | S3, S9 | inline | pending |
| T2 | lobby screen: wordmark, instance list, status panel, footer help | S2, S8 | inline | pending |
| T3 | actions: start, stop, backup through async commands with spinner and inline errors | S4, S5, S7, S9 | inline | pending |
| T4 | logs screen with tail, scroll and refresh | S6 | inline | pending |
| T5 | `internal/app/tui.go` adapter and the TTY gate in `Run` | S1, S10 | inline | pending |
| T6 | README, `docs/design.md` and `AGENTS.md` | S11 | inline | pending |

## Log

- **L1** (user, verbatim): "uff excelente pude entrar a mi partida, aunque veo algo que siento que
  seria tosco apra gente que no se especializa en al consola y es, que al arrancarlo o algo no
  existe un lobby, como asi? un inicio ne la consola que me diga timpo GAMEHOST arriba en grande y
  me muestre los comandos moviendome con flechas yo, es decir las opciones mas didacticas sabes?
  es mi idea, no se que dices tu?"
- **L2** (user, verbatim): "que tanto, si con buble Tea es mejor, aunque tengamos que meter mas
  archivos al proyecto, me sirve, me gusta que quede bien y optimizado, siguiendo ojo nuestras
  reglas SKILLS ante todo, y los cambios bien documentados y todo ordenado"
- **L3** — Verified against the module proxy and the downloaded source: Bubble Tea v2.1.1 declares
  its path as `charm.land/bubbletea/v2`; Lip Gloss v2.0.6 as `charm.land/lipgloss/v2`. v2 API
  differs from v1: `Init() Cmd`, `Update(Msg) (Model, Cmd)`, `View() View`, `View.AltScreen`,
  `tea.NewView`, `tea.Quit()` is a `Msg` and `Cmd` is `func() Msg`.
- **L4** — The command layer already exposes typed helpers: `startDetached`, `stopInstance`,
  `backupInstance`, `prepareStart`, `loadInstance`, `requireStopped`. The lobby consumes those
  instead of parsing flags.
