# TODO

In priority order.

## Tracking changes

1. Detect merges more widely: against the default branch as well as the base, with `git cherry`
   to count commits that landed by patch id (cherry-picks, rebases) and show a partly merged change
   as "N of M commits landed", by content for squash merges, and from the last recorded head once
   the branch is deleted.

## State

2. Remember where you were per change, the view, cursor, folds, and split mode, so reopening gr
   after an accidental q puts you back.
3. Open the overview where you last were, or at its newest end, with the change's details and
   thread counts fixed at the top like the title bar, so they stay put while scrolling.

## Agents

4. Give the gr and gr-review skills different `GR_AUTHOR` names, such as `claude` and
   `claude-review`, so a change one agent reviewed and another fixed does not read as one author
   talking to itself.
5. Stop agents writing as the human. Without `GR_AUTHOR`, gr uses the git identity, which for an
   agent is the human's, and the evals' baselines did exactly that.
6. Keep agents from reading the thread list twice. When the open threads' JSON runs past what a tool
   shows of a command's output, about 30,000 characters, agents with the skill list them again to
   read them from a file; the huge-review eval shows it. A compact list, or the skill saying to
   save a long list once, would avoid it.

## Config

7. Remappable keys, from the action registry.
8. Configurable colors: override roles, such as danger, or individual elements, over the built-in
   theme `ui.theme` picks, and glyphs over the glyph set. gr draws its own diff colors, so git's
   `color.diff.*` do not apply.

## Features

9. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
   skipping past them is obvious.
10. Comment on lines outside diff hunks, on context opened from a fold.
11. Test mouse handling in a real terminal.
12. Suggestions on line comments, as GitHub's: a ```` ```suggestion ```` block the author can apply,
    over one line or several, filled in with the selected lines to edit.
13. The mouse in the comment box and other text boxes: click to move the cursor, drag to select.
14. Select text in a diff with the mouse, to copy it or act on it.
15. Tabs in the TUI, and switching the repository or change from inside it, so several reviews run
    in one gr rather than one instance each.
16. gr on the web, or as an app in a native shell, through
    [go-booba](https://github.com/NimbleMarkets/go-booba) or similar. Needs the tabs above, to run
    several reviews in one window.

## Polish

17. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
