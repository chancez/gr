# TODO

Sections are in priority order, and so are the items within them.

## Now: the review loop between a human and an agent

1. Agent skills in `skills/gr` and `skills/gr-review`: test them with skill-creator's evals.
2. Show threads anchored to commits in the whole diff too, wherever their code still is, so
   unresolved feedback is visible there.
3. Jump from a thread in the overview to its code in the latest whole diff, to check a fix.
4. A jump list, like vim's: `ctrl+o` and `ctrl+i` move back and forward between jump points.
5. Mouse wheel scrolls smoothly and leaves the selection alone; `j`/`k` then select from the first
   visible comment.

## Next: friction in daily use

6. A "more above/below" indicator on the sidebar lists when entries are scrolled out of view.
7. Drag the divider between the commit list and the file list.
8. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
   skipping past them is obvious.
9. A style registry: every element drawn in the UI (author, sha, verdict, path, ...) gets its style
   from one place, so the same thing looks the same in every pane. Design first.
10. Right-click menus with the actions for what was clicked.
11. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
    reviewed marks survive squashing fixups.
12. Comment on lines outside diff hunks, on context opened from a fold.
13. Load commit diffs lazily. Every diff is loaded up front, so large branches are slow to open.
14. Reviewed marks from the CLI, for agents that review.
15. Test mouse handling in a real terminal.

## Config

16. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
17. Remappable keys. Bindings already live in one keymap in `internal/tui/keys.go`.
18. Configurable colors: choose a built-in theme (default or colorblind, today from `GR_THEME`) and
    override individual colors. gr draws its own diff colors, so git's `color.diff.*` do not apply.
19. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
20. Configurable scrolloff and default sidebar width.

## Polish

21. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.
22. Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
23. Detect squash merges as merged.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
