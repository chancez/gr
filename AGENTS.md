# AGENTS.md

This file provides guidance to coding agents, working in this repository.

gr is local, GitHub-style code review for git: a Bubble Tea TUI for reviewing, and a CLI with
`--json` for agents and editors. README.md covers usage; docs/design.md the why; docs/adr the
decisions; GLOSSARY.md the terms (change, revision, thread, anchor, ...); TODO.md the plan, in
priority order.

## Commands

Tools come from mise (`mise.toml` pins Go).

```
mise run build        # bin/gr
mise run install      # build, then install to ~/.local/bin/gr
mise run check        # go fmt, go vet, go test
mise run test-race
go test ./internal/tui -run TestControls          # one test
go test ./internal/tui -run TestGoldenScreens -update   # rewrite golden screens
```

`gofmt -l` exits 0 even when it lists files, so check its output, not its status.

## Architecture

Dependencies point down: `cli` and `tui` -> `review` -> `store`, `git`, `anchor`. The front ends
never touch `store` or `git` directly, so the CLI and TUI behave the same.

- `internal/review`: the domain. Changes and their revisions (recorded when a change is accessed,
  ADR 7), reviews, threads, reviewed marks, and re-anchoring threads onto new revisions by content
  (ADR 5). A commit message is reviewed as the file `/COMMIT_MSG` (ADR 6).
- `internal/store`: one SQLite database for every repo (`modernc.org/sqlite`, no cgo), keyed by
  the repo's root commit, so clones and worktrees share reviews. Migrations are embedded and run on
  open. UUIDv7 keys; revisions and threads also get a sequence number within their change (ADR 4).
- `internal/git`: shells out to `git` (ADR 2) and parses unified diffs. Diffs load lazily (ADR 9).
- `internal/cli`: commands and their human and JSON output.

The TUI polls `PRAGMA data_version` to reload when another process writes.

### TUI (`internal/tui`)

- `look` is how things are drawn. Text is spans of semantic elements drawn on surfaces (ADR 8); a
  surface owns the background. Clickable spans are marked with zero-width APC zone sequences that
  `look.Scan` strips from the final frame, recording where each landed. Restyling a line must keep
  marks (`look.KeepMarks`).
- Symbols are roles (`look.Glyph`), drawn from the theme's glyph set: ASCII, Unicode (the
  default), or Nerd Font icons where the terminal is known to bundle them (`look/glyphs.go`).
  Glyphs are written as `\u` escapes, so the source stays ASCII. A bare `look.Theme`, and the
  test model, draw ASCII.
- `interact` is how things are acted on (ADR 10). Typed `Action[M,S]`s, `Kind`s (what a subject
  is), and `Place`s (where it is: a list, or a mention inside something else). Nothing has a name;
  everything is used by address. A key goes to the cursor's subject in its place, then its kind,
  then what the kind carries (`Via`), then the pane, the change, and the app. Help, hints, menus,
  the ctrl+p palette, and clicks are all derived from this registry.
- `registry.go` declares the TUI's actions, kinds, places, and bindings. Kinds, places, and the
  controls in `controls.go` are made in `init()`: building them in their declarations makes an
  initialization cycle, since actions run code that redraws rows.
- Controls (`controls.go`) draw an action as a clickable button found on the same target its key
  runs on. Zone ids name where a control is plus its action's address, never its order in a
  frame, because `pane` caches rendered rows across frames. A control runs only in the mode it was
  drawn for. Chips around the content; quiet links inside the diff and overview, lit under the
  cursor or mouse in room kept for the key so nothing moves. A control must never add a line to a
  row: row heights are measured once in `pane.layout`.
- The main pane is a list of `row`s (`rows.go`, `overview.go`) rendered by `render.go`. Rows other
  than the cursor's, the selection's, the active hunk's, and the hovered one are cached.

## Tests

- Tests never see the user's git config or settings: `internal/gittest` sets `GIT_CONFIG_GLOBAL`
  and friends per test, and builds throwaway repos.
- TUI tests drive the real model: `newTUIEnv` or `goldenEnv` (a change with something of
  everything), then `press`, `typeText`, `openNav`, `cursorTo`, and for the mouse `click`,
  `hoverRow` (controls_test.go).
- Golden screens in `internal/tui/testdata/golden` are compared cell by cell, colors included,
  after the zone scan. Regenerate with `-update` and look at the stripped output before accepting.
- Use `testing/synctest` for anything timing-dependent.

## Skills

`skills/gr` and `skills/gr-review` are the agent skills for addressing and giving reviews; they
are symlinked into `~/.claude/skills`. `skills/gr/evals` holds eval fixtures (`fixtures.sh`, which
puts a logging `gr` shim on each fixture's PATH) and `grade.py`. Eval runs go in the gitignored
`skills/gr-workspace`.
