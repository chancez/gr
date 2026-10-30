# TODO

Sections are in priority order, and so are the items within them.

## Now: the review loop between a human and an agent

1. Agent skills in `skills/gr` and `skills/gr-review`: test them with skill-creator's evals.

## Next: friction in daily use

2. Remember where you were per change, the view, cursor, folds, and split mode, so reopening gr
   after an accidental q puts you back.
3. Next and previous unreviewed commit.
4. Expand and collapse every thread in the overview, and every thread of one review from its header.
5. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
   skipping past them is obvious.
6. A style registry: every element drawn in the UI (author, sha, verdict, path, ...) gets its style
   from one place, so the same thing looks the same in every pane. Design first.
7. Right-click menus with the actions for what was clicked.
8. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
   reviewed marks survive squashing fixups.
9. Comment on lines outside diff hunks, on context opened from a fold.
10. Load commit diffs lazily. Every diff is loaded up front, so large branches are slow to open.
11. Reviewed marks from the CLI, for agents that review.
12. Test mouse handling in a real terminal.

## Config

13. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
14. Remappable keys. Bindings already live in one keymap in `internal/tui/keys.go`.
15. Configurable colors: choose a built-in theme (default or colorblind, today from `GR_THEME`) and
    override individual colors. gr draws its own diff colors, so git's `color.diff.*` do not apply.
16. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
17. Configurable scrolloff and default sidebar width.

## Polish

18. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.
19. Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
20. Detect squash merges as merged.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
