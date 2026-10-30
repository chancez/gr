# TODO

In priority order. Foundations come first because every feature built before them is more to redo.

## Bugs

1. Escape control characters in diffs, commit messages, and comments before drawing them. A file
   with terminal escapes in it, such as the golden screens, sends them straight to the terminal.

## Foundations

2. Load diffs lazily into a bounded cache and reload only what changed, per
   [ADR 9](docs/adr/0009-lazy-diffs.md), so actions show at once and large changes stay fast.
3. Harder skill evals, in `skills/*/evals`. Iteration 1 only separated the skills from no skill on
   `GR_AUTHOR`. Next: an outdated thread, an ask narrowed by a later reply, running off the change's
   branch, open threads mixed with addressed ones, and a fix touching code under another open thread.
4. Draw the TUI from registered elements, per [ADR 8](docs/adr/0008-tui-elements.md): every element
   (author, sha, verdict, path, ...) styled from one place, so it looks the same in every pane.
5. An action registry, with an ADR first: each action's name, key, description, where it applies,
   and what it runs, in one place. The hint bar and help come from it, and later remappable keys,
   right-click menus, and a command palette.

## Quick wins, on the new foundations

6. Expand and collapse every thread in the overview, and every thread of one review from its header.
7. Next and previous unresolved thread, across views.
8. Reviewed marks from the CLI, for agents that review.
9. Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
10. Open the file under the cursor in `$EDITOR` at the cursor's line: as it was at the commit being
   reviewed, a read-only copy from `git show`, or the working tree's file in the whole diff.
11. Diff size like GitHub's: `+N -M` totals for a commit and the whole diff in the pane title, and per
    file on its header and in the file list. `--numstat` alongside the eager `git log --raw` gives
    them in the same process, without loading diffs.
12. Stop agents writing as the human. Without `GR_AUTHOR`, gr uses the git identity, which for an
    agent is the human's, and the evals' baselines did exactly that.
13. Dim what is finished: a resolved thread, its state included, and a review's header once its
    summary and threads are all resolved.

## State

14. Remember where you were per change, the view, cursor, folds, and split mode, so reopening gr
   after an accidental q puts you back.
15. Open the overview where you last were, or at its newest end, with the change's details and
    thread counts fixed at the top like the title bar, so they stay put while scrolling.

## Tracking changes

16. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
    reviewed marks survive squashing fixups.
17. Detect merges more widely: against the default branch as well as the base, with `git cherry`
    to count commits that landed by patch id (cherry-picks, rebases) and show a partly merged change
    as "N of M commits landed", by content for squash merges, and from the last recorded head once
    the branch is deleted.

## Config

18. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
19. Remappable keys, from the action registry.
20. Configurable colors: choose a built-in theme (default or colorblind, today from `GR_THEME`) and
    override individual elements' colors. gr draws its own diff colors, so git's `color.diff.*` do
    not apply.
21. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
22. Configurable scrolloff and default sidebar width.
23. Configurable default for auto-advance after marking reviewed, which `+` toggles per session.

## Features

24. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
    skipping past them is obvious.
25. Right-click menus with the actions for what was clicked, from the action registry.
26. Comment on lines outside diff hunks, on context opened from a fold.
27. Test mouse handling in a real terminal.
28. Vim keys in the comment editor, without leaving for `$EDITOR`: normal and insert modes over the
    text area, with the common motions and operators, turned on in the config.

## Polish

29. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
