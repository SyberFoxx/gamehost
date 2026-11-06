# The lobby is the whole program

Turn the interactive lobby into the complete entry point: every command the project ships must be
reachable inside the TUI, and the TUI must be the first thing a person sees. Branch
`feat/tui-complete`, base `feat/tui-lobby` (PR #1), delivery `feature-branch-chain`, test runner
`go test -race ./...`.

## Specs

- **S1** — `gamehost` with no arguments on a terminal opens the lobby, and every capability of the
  project is reachable from it without typing a command. Any other stdout keeps the current usage
  text and exit code 2. User words: "que el sistema para funcionar sea si o si abrir el TUI y
  funcione por el" and "que ese TUI funcione si o si siempre, para poder correr el programa y todo
  sea si o si inicial TUI".
- **S2** — Instance management lives in the lobby: list, create, inspect and edit. Nothing about an
  instance needs `gamehost create`, `show` or `config` any more. User words: "necsitamos que este
  completo con todo lo que damos y proponemos, es una app de consola claro".
- **S3** — The create wizard asks for the same settings as `create`, validates them with the same
  rules and messages, and refuses a name that already exists. It works for both supported games and
  hides the fields the chosen game does not use.
- **S4** — The settings screen shows the current values of an instance, saves an edited copy, and
  refuses to save while the server is running, with the same message `config` prints.
- **S5** — `install` and `update` run from the lobby and stream the SteamCMD output line by line into
  a progress screen; `update` keeps its safety archive; the operation can be cancelled with `esc`
  and the cancelled context stops the child process.
- **S6** — Admins are managed in the lobby: list, add with an optional display name and limited
  privileges, and remove. The stopped-only rule and the Steam ID validation are the same as
  `admin`.
- **S7** — Backups are managed in the lobby: list the archives with date and size, create one,
  restore a selected archive, and import a `.world.gzip` by path. Restore and import keep the
  pre-restore safety archive and refuse a running server.
- **S8** — Logs open with the newest tail and then follow the file live; new lines appear without
  pressing anything, the view sticks to the bottom while following, and `esc` stops the follower
  before returning.
- **S9** — The systemd units are managed in the lobby: show the unit names and whether they are
  active, install (with or without enabling) and uninstall.
- **S10** — `doctor` runs in the lobby and shows every check with its status and detail, plus a
  clear healthy/unhealthy verdict.
- **S11** — The web panel can be started and stopped from the lobby, shows the address it listens
  on, and dies with the lobby when the lobby exits.
- **S12** — Destructive actions ask before running: stop, update, restore, import, uninstall and
  remove-admin. The dialog names the instance and what will happen.
- **S13** — `?` opens a key map, and every action shows the command line equivalent it runs, so the
  lobby keeps teaching.
- **S14** — No existing command, flag, output line or exit code changes. `go test -race ./...` stays
  green, `go vet ./...` and `gofmt -l .` stay clean.
- **S15** — Documentation ships with the code: the lobby section in `README.md`, milestone M13 and
  the new decisions in `docs/design.md`, the traps in `AGENTS.md`, and this document kept current.

## Decisions

- The lobby stays presentation only. `internal/tui` imports nothing from the project; the adapter
  `internal/app/tui*.go` fills a `Backend` of function-valued groups, one group per concern, so a
  screen can be tested with only the group it uses.
- A screen stack replaces the two-value `screen` enum. Each screen implements three methods
  (`Init`, `Update`, `View`), which keeps the contract inside the project rule for small
  interfaces, and the root model owns navigation, the help overlay and the confirm dialog.
- Long operations return a `<-chan Event` and are consumed with one command per event, the canonical
  Bubble Tea pattern. No goroutine writes to the model, and `esc` cancels the context that the child
  process already honours.
- `charm.land/bubbles/v2` joins the dependencies for `textinput` only: forms need a real text editor
  with cursor movement, backspace and Unicode, and hand-rolling that is the kind of code that looks
  right and breaks on an accented password.
- `serve` starts inside the lobby as a goroutine over `httpapi.New` plus `net.Listen`, the same
  constructor the command uses. The screen owns the stop function, so leaving the screen or the
  lobby shuts the listener down.
- Destructive confirmations replace the screen instead of compositing an overlay. A full-screen
  dialog is easier to read and needs no cell-level math over styled content.

## Tasks

| ID | Deliverable | Specs | Route | Status |
| --- | --- | --- | --- | --- |
| T1 | typed helpers in `internal/app` (create, config, admin, service, panel) extracted from the flag wrappers, no behaviour change | S14 | inline | done |
| T2 | `Backend` split into groups, `Event`, screen stack, navigation messages, help overlay, confirm dialog | S1, S12, S13 | inline | done |
| T3 | lobby menu with sections and scrolling, migrated to the stack | S1, S2 | inline | done |
| T4 | instance detail screen | S2 | inline | done |
| T5 | generic form screen plus the create wizard | S3 | inline | done |
| T6 | settings screen on the same form | S4 | inline | done |
| T7 | install/update progress screen over the streaming backend | S5 | inline | done |
| T8 | live log follow | S8 | inline | done |
| T9 | admins screen | S6 | inline | done |
| T10 | backups screen: list, create, restore, import | S7 | inline | done |
| T11 | doctor screen | S10 | inline | done |
| T12 | systemd screen | S9 | inline | done |
| T13 | web panel screen | S11 | inline | done |
| T14 | README, `docs/design.md`, `AGENTS.md` | S15 | inline | done |
| T15 | parity with the remaining flags: `logs -n`, `backup --keep`, `service --dir/--binary/--systemctl`, `serve --start`, `version` | S2, S14, S15 | inline | done |

Work units follow the task table. PR slices, in `feature-branch-chain` order:

- **A** — T1–T3 (backbone). New PR targets `feat/tui-lobby`.
- **B** — T4–T8 (instance management and operations). Targets slice A.
- **C** — T9–T14 (environment, docs). Targets slice B.

## Log

- **L1** (user, verbatim): "buenas, vamos a continuar con el TUI, que el sistema para funcionar sea
  si o si abrir el TUI y funcione por el, con nuestras reglas que conocemos y con lo que sabes del
  proyecto, de igua lforma revisa como esta todo en agents.md y toods nuestors .md excelentes, para
  seguir donde quedamos que ese TUI funcione si o si siempre, para poder correr el programa y todo
  sea si o si inicial TUI, para dar por excelente el sistema para Corekeper e ir escalando a otros
  servers"
- **L2** (user, verbatim, previous turn): "ok, se mira excelente, que mas podemos mejorarle a ese
  TUI, necsitamos que este completo con todo lo que damos y proponemos, es una app de consola claro"
- **L3** — Audit of `AGENTS.md`, `README.md`, `docs/design.md`: the milestone table ends at M12, the
  command table lists every command, and the non-goals still say "a full-screen dashboard with
  editable forms" is out of scope. That bullet is what this feature removes; M13 replaces it.
- **L4** — Command surface to cover: `doctor`, `install`, `update`, `create`, `list`, `show`,
  `config`, `admin list/add/remove`, `start`, `stop`, `logs`, `backup`, `restore`, `import-world`,
  `serve`, `service install/uninstall`, `status`, `version`. `list` is the lobby itself and `start`
  only needs its detached form; everything else needs a screen.
- **L5** — `charm.land/bubbles/v2@v2.2.1` uses the same `charm.land` module root as Bubble Tea v2 and
  exposes `textinput.Model` with `Update`/`View`/`Value`/`SetValue`. Verified in the module cache
  before adding it.
- **L6** — Review before the docs: the form handled `left`/`right` for every field, so a text input
  never saw its own horizontal arrows and a typo in the middle of a password could not be fixed.
  The keys now reach the input unless the field is a choice; `TestFormKeepsArrowsInsideTextFields`
  covers it.
- **L7** — `gofmt -l .`, `go vet ./...` and `go test -race ./...` pass with the new test. The TTY
  gate is unchanged, so the systemd units and `serve` keep the behaviour they had.
- **L8** — The branch now carries the code, the tests and the documentation. Pushed to `origin`;
  the repository stays under the owner's account with no additional collaborators.
- **L9** — SteamCMD installed at `~/.local/share/steamcmd` and exercised for real: the first run
  self-updates to client `1792617903`; `gamehost install corekeeper` ends with
  `Success! App '1963720' fully installed.` and a 568 MB `files/`; `gamehost update corekeeper`
  writes a 4.7 MB `pre-update_<timestamp>.tar.gz` and revalidates in 6.3 s on a warm cache.
- **L10** — A pty harness (`python3` + `pty`) drove the real lobby with that instance: 22 checks
  passed, including the streamed SteamCMD install, the log tail reload, a real backup through the
  retention form and the panel listening on `127.0.0.1:8080`. No test in the repository needs a
  TTY; this was a manual smoke run.
- **L11** — Parity audit closed the last gaps: the log screen edits the tail size (`logs -n`), the
  backup form asks for the retention (`backup --keep`), the systemd screen edits
  `--dir/--binary/--systemctl`, the panel picks an instance to start (`serve --start`) and the
  environment screen shows the build version. Foreground `start` stays out on purpose: the alternate
  screen owns the terminal.
- **L12** — `gofmt -l .`, `go vet ./...` and `go test -race ./...` are clean; the documents carry the
  measured install, update and smoke-test facts instead of estimates.
