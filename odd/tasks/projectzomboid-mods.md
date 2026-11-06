# Project Zomboid mods (M16)

Goal: manage the mods a Project Zomboid server loads without asking the player to edit the ini, and
without letting a stale copy live in `instance.json`.

## Contract

The dedicated server reads two keys at the top of `Server/<name>.ini`:

- `WorkshopItems=`: Steam Workshop ids, digits, semicolon separated.
- `Mods=`: mod folder names, semicolon separated.

There is no launch flag for either, and the server rewrites the file while keeping unknown lines.
The ini is created on the first start, so a missing file means the server never ran.

## What was built

- `internal/projectzomboid/mods.go`: reads both keys and rewrites only those two lines in place.
  Everything else stays byte for byte; a missing ini is an error that names the reason.
- `game.ModStore`: optional driver interface with `Mods` and `SaveMods`. The CLI and the lobby use it
  through a type assertion, never a `switch` on the game name.
- `mods list|add|remove`: `--workshop` selects the workshop list; the default is a mod id. `list`
  works while running, add and remove take the instance lock and refuse a live server.
- Lobby: a `Mods` screen with `a` (mod id), `w` (workshop item), `d` (remove), plus the same
  validation and confirmation as the CLI.
- Validation: workshop ids are digits, mod ids are letters, digits, `_`, `-` or `.`, both are
  deduplicated before writing.

## Log

- **P1** — Unit tests cover reading, appending missing keys, preserving unrelated lines, rejecting a
  missing ini, deduplicating and the id rules.
- **P2** — End-to-end CLI test: create a Project Zomboid instance, write a realistic ini, add a
  workshop item and a mod, confirm the other lines are intact, then remove one and confirm the file.
  Core Keeper correctly refuses the command.
- **P3** — Not verified with a live Project Zomboid server on this run: installing the ~4 GB app and
  loading a mod needs a Steam Workshop download. The file format and line preservation are covered
  by the tests above, and the design keeps the server's own writes authoritative.
