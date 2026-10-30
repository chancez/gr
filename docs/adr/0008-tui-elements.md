# 8. Draw the TUI from registered elements

Status: accepted

## Context

Each pane styles its own text. The same thing looks different from pane to pane: the author is
colored in reviews but not in the commit header, the sha in the review header but not the sidebar.
Each composed line also has to repeat its background on every styled piece, and getting that wrong
has caused one bug after another in header bars, selected rows, and badges. Colors are meant to be
configurable, which needs names for what is being colored.

## Decision

The TUI is drawn from **elements**, much like components in React: named kinds of things on
screen, such as author, sha, verdict, path, thread ref, key, hint text, and timestamp.

- **One registry.** `styles.go` maps each element to its default style, per theme. An element is
  drawn with that style wherever it appears.
- **Props and overrides.** Like a component's props, an element can take options where a place needs
  something different, such as dimmed for a reviewed entry or emphasized in a header. The registry
  defines what each option does to the style, so a place asks for "author, dimmed" rather than
  choosing a color. Themes and the config file override an element's style in the same registry.
- **A line is a list of elements, not styled text.** Code that draws a row says what is on it, for
  example "the sha, then the subject, then the author", without any colors. One function then
  draws that line: it looks up each element's style and adds the background of where the line
  sits, such as a header bar, the cursor, or a selection. Each piece of text no longer has to
  repeat the background itself, which is where the bugs came from.
- **Enforced.** A test fails when code outside `styles.go` creates a style or sets a color. A new
  kind of thing on screen gets an element before it can be drawn.

## Consequences

- The same thing looks the same in every pane by default, and differs only through the options the
  registry defines.
- Background, cursor, and selection handling live in one place.
- The config file overrides colors by element name, for example `author` or `sha`.
- Adding an element is a registry edit, a little more work than styling text in place. Changing how
  an element looks is then one edit, rather than one per place it is drawn.
- Moving the existing panes over is a refactor of the renderer, the sidebar, and the overview.
