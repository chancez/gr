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

- `look` is how things are drawn (ADR 8): semantic elements on surfaces, which own the background,
  and symbols as roles drawn from a glyph set (ASCII, Unicode, or Nerd Font). Clickable spans
  carry zero-width zone marks that the final frame is scanned for, so a click maps back to what
  was drawn there.
- `interact` is how things are acted on (ADR 10): typed actions on kinds of subject, bound to keys
  on kinds and the places subjects appear. Help, hints, menus, the palette, and clicks all come
  from this one registry, declared in `registry.go`. Kinds and places are made in `init()`, as
  making them in their declarations is an initialization cycle.
- Controls draw an action as something to click, found on the same target its key runs on, so a
  click always does what the key does there.
- The main pane is a list of `row`s whose drawings are cached across frames and whose heights are
  measured once per layout. So nothing drawn on screen may depend on its order within a frame, and
  nothing may change a row's height without a new layout.

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
