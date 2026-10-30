# 8. Draw the TUI from registered elements

Status: accepted

## Context

Each pane styles its own text, so the same thing looks different from pane to pane: the sha is
yellow in the commit header and muted in a thread's location, the author purple in threads and
muted in the commit header. Each composed line also repeats its background on every styled piece,
and getting that wrong has caused one bug after another in header bars, selected rows, and badges.
Where a background would clash, the code strips the colors instead, so a thread's state is colored
in its box and plain once collapsed. Colors are meant to be configurable, which needs names for
what is being colored.

## Decision

The TUI is drawn from **elements**, much like components in React, on **surfaces**, from one
registry in `styles.go` with three layers.

- **Palette.** Named colors: foreground, muted, accent, green, red, yellow, purple, border, and the
  backgrounds. A theme is a palette; the colorblind theme swaps green and red for blue and orange
  without touching the elements.
- **Elements.** A named kind of text on screen, such as author, sha, timestamp, path, thread ref,
  thread state, verdict, key, and hint. An element sets a foreground and attributes, never a
  background. An element whose look depends on a value has variants, such as `verdict.approve` or
  `thread-state.resolved`.
- **Props.** Like a component's props, a place can ask for an element with options, and the
  registry defines what each does: `dim` for reviewed, collapsed, or resolved entries, and `strong`
  for headers. A place asks for "author, dimmed" rather than choosing a color. Options are added
  when a place needs one, not ahead of time.
- **Surfaces.** Where a line sits: plain, heading bar, file header, hunk header, pane title focused
  or unfocused, cursor, dim cursor, selection, added and deleted lines, and status messages. A
  surface owns the background. A soft surface, such as the cursor or a heading bar, keeps its
  elements' colors. A solid one, such as the focused pane title, draws them all in its own
  foreground, since colored text on a solid accent is unreadable.
- **A line is a list of elements, not styled text.** Code that draws a row says what is on it, for
  example "the sha, then the subject, then the author", without any colors. One function draws
  that line on a surface at a width: it styles each element, applies the surface, and pads to the
  width with the surface's background.
- **Foreign renderers plug in.** Syntax highlighting and Markdown keep their own styling and join a
  line as already-styled pieces. Highlighting takes the surface's background; Markdown only sits on
  the plain surface.
- **Layout stays in code.** Borders, padding, indents, and widths are not styles. Only their colors
  come from the registry.
- **Enforced.** A test fails when code outside `styles.go` creates a style or sets a color, other
  than the two foreign renderers. A new kind of thing on screen gets an element before it can be
  drawn.
- **Names match the config.** Elements and surfaces have dotted names, such as `author`,
  `verdict.approve`, and `surface.cursor`, which the config file will use to override them.

## Consequences

- The same thing looks the same in every pane by default, and differs only through the props the
  registry defines.
- Background, cursor, and selection handling live in one place, and badges keep their colors on
  the cursor and on heading bars.
- Adding an element is a registry edit, a little more work than styling text in place. Changing how
  an element looks is then one edit, rather than one per place it is drawn.
- Moving the existing panes over is a refactor of the renderer, the sidebar, and the overview.
  Golden tests of rendered screens, with their colors, come first, so each pane moves in its own
  commit with no change to the output. Changes to how things look, such as one sha color
  everywhere, follow in their own commits.
