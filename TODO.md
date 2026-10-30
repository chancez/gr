# TODO

Sections are in priority order, and so are the items within them.

## Now: the review loop between a human and an agent

1. Agent skills in `skills/gr` and `skills/gr-review`: test them with skill-creator's evals.

## Next: friction in daily use

2. Remember where you were per change, the view, cursor, folds, and split mode, so reopening gr
   after an accidental q puts you back.
3. Next and previous unreviewed commit.
4. Next and previous unresolved thread, across views.
5. Submit without a summary in one step: in the verdict prompt, A, R, and C approve, request
   changes, or comment straight away, with a hint that says so.
6. Open the overview where you last were, or at its newest end, with the change's details and
   thread counts fixed at the top like the title bar, so they stay put while scrolling.
7. Expand and collapse every thread in the overview, and every thread of one review from its header.
8. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
   skipping past them is obvious.
9. Draw the TUI from registered elements, per [ADR 8](docs/adr/0008-tui-elements.md): every element
   (author, sha, verdict, path, ...) styled from one place, so it looks the same in every pane.
10. Right-click menus with the actions for what was clicked.
11. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
   reviewed marks survive squashing fixups.
12. Comment on lines outside diff hunks, on context opened from a fold.
13. Load commit diffs lazily. Every diff is loaded up front, so large branches are slow to open.
14. Reviewed marks from the CLI, for agents that review.
15. Test mouse handling in a real terminal.
16. Speed up feedback after an action. Each action already reloads at once, but a reload re-runs git
    for everything: a diff per commit (66 diffs take about 0.5s on this branch), the whole diff, the
    interdiff, and one diff per thread placed in the whole diff. Reload only the database when git
    has not moved, and apply the change to the screen before the reload lands.
17. Log with `log/slog` to a file under `$XDG_STATE_HOME/gr/`, since the TUI owns the terminal: git
    commands and their timing, database writes, reloads, and errors, so problems can be diagnosed
    after the fact. A `--debug` flag or `GR_LOG` sets the level.

## Config

18. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
19. Remappable keys. Bindings already live in one keymap in `internal/tui/keys.go`.
20. Configurable colors: choose a built-in theme (default or colorblind, today from `GR_THEME`) and
    override individual colors. gr draws its own diff colors, so git's `color.diff.*` do not apply.
21. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
22. Configurable scrolloff and default sidebar width.

## Polish

23. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.
24. Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
25. Detect squash merges as merged.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
