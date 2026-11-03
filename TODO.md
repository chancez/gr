# TODO

In priority order. Foundations come first because every feature built before them is more to redo.

## Foundations

1. Keep agents from reading the thread list twice. When the open threads' JSON runs past what a tool
   shows of a command's output, about 30,000 characters, agents with the skill list them again to
   read them from a file; the huge-review eval shows it. A compact list, or the skill saying to
   save a long list once, would avoid it.

## Quick wins, on the new foundations

2. Give the gr and gr-review skills different `GR_AUTHOR` names, such as `claude` and
   `claude-review`, so a change one agent reviewed and another fixed does not read as one author
   talking to itself.
3. Stop agents writing as the human. Without `GR_AUTHOR`, gr uses the git identity, which for an
   agent is the human's, and the evals' baselines did exactly that.

## State

4. Remember where you were per change, the view, cursor, folds, and split mode, so reopening gr
   after an accidental q puts you back.
5. Open the overview where you last were, or at its newest end, with the change's details and
   thread counts fixed at the top like the title bar, so they stay put while scrolling.

## Tracking changes

6. Start a thread on the latest revision when a new one was recorded while it was being placed:
   StartThread works out its location outside the write transaction and saves it on the revision
   it read first, so it stays on the old one until the next.
7. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
   reviewed marks survive squashing fixups.
8. Detect merges more widely: against the default branch as well as the base, with `git cherry`
   to count commits that landed by patch id (cherry-picks, rebases) and show a partly merged change
   as "N of M commits landed", by content for squash merges, and from the last recorded head once
   the branch is deleted.

## Config

9. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
10. Remappable keys, from the action registry.
11. Configurable colors: choose a built-in theme (default or colorblind, today from `GR_THEME`), and
    override roles, such as danger, or individual elements. gr draws its own diff colors, so git's
    `color.diff.*` do not apply.
12. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
13. Configurable scrolloff and default sidebar width.
14. Configurable default for auto-advance after marking reviewed, which `+` toggles per session.

## Features

15. Keep the header of the file at the top of the diff pinned, like GitHub's sticky file headers, so
    the file being read is always named while scrolling through it.
16. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
    skipping past them is obvious.
17. Comment on lines outside diff hunks, on context opened from a fold.
18. Test mouse handling in a real terminal.
19. Suggestions on line comments, as GitHub's: a ```` ```suggestion ```` block the author can apply,
    over one line or several, filled in with the selected lines to edit.
20. A visible control for most actions, so the mouse reaches them without a menu: save and discard
    on the comment box and in the unsaved comment prompt, a + in a diff line's left margin to comment
    on it as on GitHub, and so on through the registry's actions.
21. The mouse in the comment box and other text boxes: click to move the cursor, drag to select.
22. Select text in a diff with the mouse, to copy it or act on it.
23. Tabs in the TUI, and switching the repository or change from inside it, so several reviews run
    in one gr rather than one instance each.
24. gr on the web, or as an app in a native shell, through
    [go-booba](https://github.com/NimbleMarkets/go-booba) or similar. Needs the tabs above, to run
    several reviews in one window.
25. Vim keys in the comment editor, without leaving for `$EDITOR`: normal and insert modes over the
    text area, with the common motions and operators, turned on in the config.

## Polish

26. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
