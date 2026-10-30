# TODO

## Config

- TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
- Configurable colors. The light and dark palettes are hard-coded in `internal/tui/styles.go`.
- Remappable keys. Bindings already live in one keymap in `internal/tui/keys.go`.
- Configurable scrolloff.
- Default sidebar width.

## TUI

- Comment on lines outside diff hunks, by expanding context.
- Load commit diffs lazily. Every diff is loaded up front, so large branches are slow to open.
- Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
  lines is colored only on its first line.
- Test mouse handling in a real terminal.

## Review

- Detect squash merges as merged.
- Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
- Track commits across rebases, for example from `post-rewrite`, so reviewed marks and threads survive
  more rewrites.

## CLI

- Reviewed marks from the CLI, for agents that review.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
