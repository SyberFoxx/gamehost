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
| T1 | typed helpers in `internal/app` (create, config, admin, service, panel) extracted from the flag wrappers, no behaviour change | S14 | inline | pending |
| T2 | `Backend` split into groups, `Event`, screen stack, navigation messages, help overlay, confirm dialog | S1, S12, S13 | inline | pending |
| T3 | lobby menu with sections and scrolling, migrated to the stack | S1, S2 | inline | pending |
| T4 | instance detail screen | S2 | inline | pending |
| T5 | generic form screen plus the create wizard | S3 | inline | pending |
| T6 | settings screen on the same form | S4 | inline | pending |
| T7 | install/update progress screen over the streaming backend | S5 | inline | pending |
| T8 | live log follow | S8 | inline | pending |
| T9 | admins screen | S6 | inline | pending |
| T10 | backups screen: list, create, restore, import | S7 | inline | pending |
| T11 | doctor screen | S10 | inline | pending |
| T12 | systemd screen | S9 | inline | pending |
| T13 | web panel screen | S11 | inline | pending |
| T14 | README, `docs/design.md`, `AGENTS.md` | S15 | inline | pending |

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
