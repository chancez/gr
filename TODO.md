# TODO

In priority order. Foundations come first because every feature built before them is more to redo.

## Foundations

1. Load diffs lazily into a bounded cache and reload only what changed, per
   [ADR 9](docs/adr/0009-lazy-diffs.md), so actions show at once and large changes stay fast.
2. Agent skills in `skills/gr` and `skills/gr-review`: test them with skill-creator's evals.
3. Draw the TUI from registered elements, per [ADR 8](docs/adr/0008-tui-elements.md): every element
   (author, sha, verdict, path, ...) styled from one place, so it looks the same in every pane.
4. An action registry, with an ADR first: each action's name, key, description, where it applies,
   and what it runs, in one place. The hint bar and help come from it, and later remappable keys,
   right-click menus, and a command palette.

## Quick wins, on the new foundations

5. Expand and collapse every thread in the overview, and every thread of one review from its header.
6. Next and previous unresolved thread, across views.
7. Reviewed marks from the CLI, for agents that review.
8. Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
9. Open the file under the cursor in `$EDITOR` at the cursor's line: as it was at the commit being
   reviewed, a read-only copy from `git show`, or the working tree's file in the whole diff.

## State

10. Remember where you were per change, the view, cursor, folds, and split mode, so reopening gr
   after an accidental q puts you back.
11. Open the overview where you last were, or at its newest end, with the change's details and
    thread counts fixed at the top like the title bar, so they stay put while scrolling.

## Tracking changes

12. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
    reviewed marks survive squashing fixups.
13. Detect merges more widely: against the default branch as well as the base, with `git cherry`
    to count commits that landed by patch id (cherry-picks, rebases) and show a partly merged change
    as "N of M commits landed", by content for squash merges, and from the last recorded head once
    the branch is deleted.

## Config

14. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
15. Remappable keys, from the action registry.
16. Configurable colors: choose a built-in theme (default or colorblind, today from `GR_THEME`) and
    override individual elements' colors. gr draws its own diff colors, so git's `color.diff.*` do
    not apply.
17. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
18. Configurable scrolloff and default sidebar width.

## Features

19. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
    skipping past them is obvious.
20. Right-click menus with the actions for what was clicked, from the action registry.
21. Comment on lines outside diff hunks, on context opened from a fold.
22. Test mouse handling in a real terminal.

## Polish

23. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
