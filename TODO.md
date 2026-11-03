# TODO

In priority order. Foundations come first because every feature built before them is more to redo.

## Foundations

1. Harder skill evals, in `skills/*/evals`. Iteration 1 only separated the skills from no skill on
   `GR_AUTHOR`. Next: an outdated thread, an ask narrowed by a later reply, running off the change's
   branch, open threads mixed with addressed ones, and a fix touching code under another open thread.
   Also check for wasted calls: agents list the open threads, then fetch a thread by ref that the
   list already showed in full.

## Quick wins, on the new foundations

2. Expand and collapse every thread in the overview, and every thread of one review from its header.
3. Next and previous unresolved thread, across views.
4. Reviewed marks from the CLI, for agents that review.
5. Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
6. Open the file under the cursor in `$EDITOR` at the cursor's line: as it was at the commit being
   reviewed, a read-only copy from `git show`, or the working tree's file in the whole diff.
7. Diff size like GitHub's: `+N -M` totals for a commit and the whole diff in the pane title, and per
   file on its header and in the file list. `--numstat` alongside the eager `git log --raw` gives
   them in the same process, without loading diffs.
8. Give the gr and gr-review skills different `GR_AUTHOR` names, such as `claude` and
   `claude-review`, so a change one agent reviewed and another fixed does not read as one author
   talking to itself.
9. Stop agents writing as the human. Without `GR_AUTHOR`, gr uses the git identity, which for an
   agent is the human's, and the evals' baselines did exactly that.
10. Compare any two revisions in the TUI: pick the older and newer revision for the interdiff, not
    only "since your review" or "since the last revision", so iterations missed while the branch
    was squashed or amended can still be read.
11. A change picker in the TUI, on the base picker's popup: open changes and branches with their
    bases, to switch between them without leaving.

## State

12. Remember where you were per change, the view, cursor, folds, and split mode, so reopening gr
    after an accidental q puts you back.
13. Open the overview where you last were, or at its newest end, with the change's details and
    thread counts fixed at the top like the title bar, so they stay put while scrolling.

## Tracking changes

14. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
    reviewed marks survive squashing fixups.
15. Detect merges more widely: against the default branch as well as the base, with `git cherry`
    to count commits that landed by patch id (cherry-picks, rebases) and show a partly merged change
    as "N of M commits landed", by content for squash merges, and from the last recorded head once
    the branch is deleted.

## Config

16. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
17. Remappable keys, from the action registry.
18. Configurable colors: choose a built-in theme (default or colorblind, today from `GR_THEME`), and
    override roles, such as danger, or individual elements. gr draws its own diff colors, so git's
    `color.diff.*` do not apply.
19. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
20. Configurable scrolloff and default sidebar width.
21. Configurable default for auto-advance after marking reviewed, which `+` toggles per session.

## Features

22. Keep the header of the file at the top of the diff pinned, like GitHub's sticky file headers, so
    the file being read is always named while scrolling through it.
23. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
    skipping past them is obvious.
24. Comment on lines outside diff hunks, on context opened from a fold.
25. Test mouse handling in a real terminal.
26. Suggestions on line comments, as GitHub's: a ```` ```suggestion ```` block the author can apply,
    over one line or several, filled in with the selected lines to edit.
27. A visible control for most actions, so the mouse reaches them without a menu: save and discard
    on the comment box and in the unsaved comment prompt, a + in a diff line's left margin to comment
    on it as on GitHub, and so on through the registry's actions.
28. The mouse in the comment box and other text boxes: click to move the cursor, drag to select.
29. Select text in a diff with the mouse, to copy it or act on it.
30. Tabs in the TUI, and switching the repository or change from inside it, so several reviews run
    in one gr rather than one instance each.
31. gr on the web, or as an app in a native shell, through
    [go-booba](https://github.com/NimbleMarkets/go-booba) or similar. Needs the tabs above, to run
    several reviews in one window.
32. Vim keys in the comment editor, without leaving for `$EDITOR`: normal and insert modes over the
    text area, with the common motions and operators, turned on in the config.

## Polish

33. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
