# TODO

In priority order.

## Tracking changes

1. Detect merges more widely: against the default branch as well as the base, with `git cherry`
   to count commits that landed by patch id (cherry-picks, rebases) and show a partly merged change
   as "N of M commits landed", by content for squash merges, and from the last recorded head once
   the branch is deleted.

## State

2. Open the overview at its newest end the first time, and keep the change's details and thread
   counts fixed at the top like the title bar, so they stay put while scrolling.

## Agents

3. Keep agents from reading the thread list twice. When the open threads' JSON runs past what a tool
   shows of a command's output, about 30,000 characters, agents with the skill list them again to
   read them from a file; the huge-review eval shows it. A compact list, or the skill saying to
   save a long list once, would avoid it.

## Config

4. Remappable keys, from the action registry.
5. Configurable colors: override roles, such as danger, or individual elements, over the built-in
   theme `ui.theme` picks, and glyphs over the glyph set. gr draws its own diff colors, so git's
   `color.diff.*` do not apply.

## Features

6. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
   skipping past them is obvious.
7. Comment on lines outside diff hunks, on context opened from a fold.
8. Test mouse handling in a real terminal.
9. Suggestions on line comments, as GitHub's: a ```` ```suggestion ```` block the author can apply,
   over one line or several, filled in with the selected lines to edit.
10. The mouse in the comment box and other text boxes: click to move the cursor, drag to select.
11. Select text in a diff with the mouse, to copy it or act on it.
12. Tabs in the TUI, and switching the repository or change from inside it, so several reviews run
    in one gr rather than one instance each.
13. gr on the web, or as an app in a native shell, through
    [go-booba](https://github.com/NimbleMarkets/go-booba) or similar. Needs the tabs above, to run
    several reviews in one window.

## Polish

14. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
