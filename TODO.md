# TODO

Sections are in priority order, and so are the items within them.

## Now: the review loop between a human and an agent

1. Agent skill in `skills/gr`, built with the skill-creator skill: find the change, read open
   threads, fix, reply, mark addressed, and review with comments and a verdict.

## Next: friction in daily use

2. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
   reviewed marks survive squashing fixups.
3. Comment on lines outside diff hunks, on context opened from a fold.
4. Load commit diffs lazily. Every diff is loaded up front, so large branches are slow to open.
5. Reviewed marks from the CLI, for agents that review.
6. Test mouse handling in a real terminal.

## Config

7. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
8. Remappable keys. Bindings already live in one keymap in `internal/tui/keys.go`.
9. Configurable colors. The light and dark palettes are hard-coded in `internal/tui/styles.go`.
10. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
11. Configurable scrolloff and default sidebar width.

## Polish

12. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.
13. Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
14. Detect squash merges as merged.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
