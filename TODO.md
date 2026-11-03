# TODO

In priority order.

## Tracking changes

1. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
   reviewed marks survive squashing fixups.
2. Start a thread on the latest revision when a new one was recorded while it was being placed:
   StartThread works out its location outside the write transaction and saves it on the revision
   it read first, so it stays on the old one until the next.
3. Detect merges more widely: against the default branch as well as the base, with `git cherry`
   to count commits that landed by patch id (cherry-picks, rebases) and show a partly merged change
   as "N of M commits landed", by content for squash merges, and from the last recorded head once
   the branch is deleted.

## State

4. Remember where you were per change, the view, cursor, folds, and split mode, so reopening gr
   after an accidental q puts you back.
5. Open the overview where you last were, or at its newest end, with the change's details and
   thread counts fixed at the top like the title bar, so they stay put while scrolling.

## Agents

6. Give the gr and gr-review skills different `GR_AUTHOR` names, such as `claude` and
   `claude-review`, so a change one agent reviewed and another fixed does not read as one author
   talking to itself.
7. Stop agents writing as the human. Without `GR_AUTHOR`, gr uses the git identity, which for an
   agent is the human's, and the evals' baselines did exactly that.
8. Keep agents from reading the thread list twice. When the open threads' JSON runs past what a tool
   shows of a command's output, about 30,000 characters, agents with the skill list them again to
   read them from a file; the huge-review eval shows it. A compact list, or the skill saying to
   save a long list once, would avoid it.

## Config

9. Remappable keys, from the action registry.
10. Configurable colors: override roles, such as danger, or individual elements, over the built-in
    theme `ui.theme` picks, and glyphs over the glyph set. gr draws its own diff colors, so git's
    `color.diff.*` do not apply.

## Features

11. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
    skipping past them is obvious.
12. Comment on lines outside diff hunks, on context opened from a fold.
13. Test mouse handling in a real terminal.
14. Suggestions on line comments, as GitHub's: a ```` ```suggestion ```` block the author can apply,
    over one line or several, filled in with the selected lines to edit.
15. The mouse in the comment box and other text boxes: click to move the cursor, drag to select.
16. Select text in a diff with the mouse, to copy it or act on it.
17. Tabs in the TUI, and switching the repository or change from inside it, so several reviews run
    in one gr rather than one instance each.
18. gr on the web, or as an app in a native shell, through
    [go-booba](https://github.com/NimbleMarkets/go-booba) or similar. Needs the tabs above, to run
    several reviews in one window.

## Polish

19. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
