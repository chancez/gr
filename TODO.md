# TODO

Sections are in priority order, and so are the items within them.

## Now: the review loop between a human and an agent

1. Agent skills in `skills/gr` and `skills/gr-review`: test them with skill-creator's evals.

## Next: friction in daily use

2. Remember where you were per change, the view, cursor, folds, and split mode, so reopening gr
   after an accidental q puts you back.
3. Next and previous unreviewed commit.
4. A key to open the overview, and from the overview to go back to where you were.
5. Next and previous unresolved thread, across views.
6. Submit without a summary in one step: in the verdict prompt, A, R, and C approve, request
   changes, or comment straight away, with a hint that says so.
7. Open the overview where you last were, or at its newest end, with the change's details and
   thread counts fixed at the top like the title bar, so they stay put while scrolling.
8. Expand and collapse every thread in the overview, and every thread of one review from its header.
9. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
   skipping past them is obvious.
10. Draw the TUI from registered elements, per [ADR 8](docs/adr/0008-tui-elements.md): every element
   (author, sha, verdict, path, ...) styled from one place, so it looks the same in every pane.
11. Right-click menus with the actions for what was clicked.
12. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
   reviewed marks survive squashing fixups.
13. Comment on lines outside diff hunks, on context opened from a fold.
14. Load commit diffs lazily. Every diff is loaded up front, so large branches are slow to open.
15. Reviewed marks from the CLI, for agents that review.
16. Test mouse handling in a real terminal.

## Config

17. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
18. Remappable keys. Bindings already live in one keymap in `internal/tui/keys.go`.
19. Configurable colors: choose a built-in theme (default or colorblind, today from `GR_THEME`) and
    override individual colors. gr draws its own diff colors, so git's `color.diff.*` do not apply.
20. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
21. Configurable scrolloff and default sidebar width.

## Polish

22. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.
23. Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
24. Detect squash merges as merged.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
