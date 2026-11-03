# gr architecture

How the code is laid out. What gr does and why is in [design.md](design.md).

## Layout

```
cmd/gr/            main; wires the CLI
internal/cli/      command definitions, human and JSON output
internal/tui/      Bubble Tea program
internal/review/   domain logic: changes, revisions, reviews, threads, re-anchoring
internal/store/    SQLite persistence and migrations
internal/git/      git subprocess wrapper and unified diff parser
internal/anchor/   anchor matching against diffs and blobs
internal/config/   the config file
```

Dependencies point down: `cli` and `tui` -> `review` -> `store`, `git`, `anchor`. `cli` and `tui`
never touch `store` or `git` directly, so both front ends behave the same.

## git

All git access goes through `internal/git`, which runs the `git` binary. Commands always pass
`--no-ext-diff --no-color` and use plumbing or stable porcelain formats such as `-z` and
`--format`. User config like the diff algorithm and rename detection is respected.

Main operations:

- repo identity: `git rev-list --max-parents=0 HEAD`, taking the oldest root. Fails on shallow
  clones (`git rev-parse --is-shallow-repository`).
- resolve refs, merge-base, list commits in a range
- diff a commit or a range, parsed into files, hunks, and lines
- read a blob or a commit message

## Store

- One database at `$XDG_DATA_HOME/gr/gr.db`, defaulting to `~/.local/share/gr/gr.db`.
- Driver: `modernc.org/sqlite`, pure Go, no cgo.
- WAL mode, a busy timeout, and foreign keys on.
- Schema migrations are embedded and applied on open, in order, recorded in a `schema_version`
  table.
- Primary keys are UUIDv7. Sequence numbers are columns allocated inside the inserting transaction.
- Paths are stored relative to the repo root. The repo's last seen path is stored as a hint.

## Live refresh

The TUI polls `PRAGMA data_version` on its own connection about once a second. A change means
another connection committed, and the TUI reloads the current view. This needs no file watching
and works with any number of writers.

## TUI

- Root model routes between views: change list, change overview, file or commit diff, thread.
- Each view is its own model. Shared components (diff viewer, Markdown renderer, comment editor)
  live in subpackages.
- Key bindings are defined once in a keymap of `bubbles/key` bindings, so help text and future
  remapping read from one place.
- Long comments can be written in `$EDITOR`.

## CLI

- Every data command takes `--json`. JSON types are defined separately from store types so the
  output contract does not change when the schema does.
- Author comes from `--author`, then `GR_AUTHOR`, then git config.
- Errors go to stderr. A non-zero exit means the command failed.

## Testing

- `internal/git` and `internal/anchor` are tested against real repositories built in temp
  directories by the tests.
- `internal/store` is tested against a temp database.
- TUI views are tested by driving models with messages and asserting rendered output.
- Concurrency and timing tests use `testing/synctest`.
