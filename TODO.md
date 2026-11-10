# TODO

In priority order.

## Tracking changes

1. Detect merges more widely: against the default branch as well as the base, show a partly merged
   change as "N of M commits landed", and find squash merges of several commits by content.

## Agents

2. Keep agents from reading the thread list twice. When the open threads' JSON runs past what a tool
   shows of a command's output, about 30,000 characters, agents with the skill list them again to
   read them from a file; the huge-review eval shows it. A compact list, or the skill saying to
   save a long list once, would avoid it.

## Config

3. Remappable keys, from the action registry.
4. Configurable colors: override roles, such as danger, or individual elements, over the built-in
   theme `ui.theme` picks, and glyphs over the glyph set. gr draws its own diff colors, so git's
   `color.diff.*` do not apply.

## Features

5. Toggle hiding reviewed commits and files, with a visible marker wherever entries are hidden, so
   skipping past them is obvious.
6. Comment on lines outside diff hunks, on context opened from a fold.
7. Test mouse handling in a real terminal.
8. Suggestions on line comments, as GitHub's: a ```` ```suggestion ```` block the author can apply,
   over one line or several, filled in with the selected lines to edit.
9. Select text in a diff with the mouse, to copy it or act on it.
10. Sessions: reopen the tabs gr had open, per session, so one gr per worktree or repository does
    not open every instance's tabs. A session could be named, or be where gr was started.
11. gr on the web, or as an app in a native shell, through
    [go-booba](https://github.com/NimbleMarkets/go-booba) or similar, running several reviews in one
    window as its tabs do.

## Polish

12. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
