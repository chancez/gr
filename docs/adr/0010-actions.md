# 10. Run the TUI's actions from one registry

Status: accepted

## Context

Every key is handled where it is pressed: a switch over the keymap for each pane and mode, which
works out what the key acts on from the cursor and then acts. The same action is written again in
each place it applies. `m` marks the file under the cursor in a diff and the selected entry in the
sidebar, through different code. Help and the hint bar are written by hand beside the switches,
and drift from them.

What an action acts on is a value: a commit, a file, a thread. The same value shows up in many
places, and ADR 8 already draws it the same way everywhere, through one constructor per kind of
value. A sha appears in the commit list, the commit header, a thread's location, and the overview,
but only the commit list lets you act on it. Nothing on screen can be clicked except the sidebar
and the picker, and a right-click menu, remappable keys, or a command palette would each need its
own copy of what every action does and where.

## Decision

Each kind of subject defines its actions and the keys bound to them, each place what its subjects
do there, and keys, menus, and the mouse all run actions through the registry.

- **Subjects and kinds.** A subject is a value an action acts on, typed by its kind. A kind is
  what a thing is: the change, the overview, the whole diff, the interdiff, a commit, a file, a
  line, a hunk, a fold of unchanged lines, a thread, a review's summary, a review, and each pane.
  Where a thing is belongs to its place, and what state it is in to its actions. So no action asks
  what sort of subject it was given, and a kind is split where its sorts share few actions: the
  overview and a commit are kinds of their own, not one kind of view. Sorts that differ only in
  what they allow stay one kind: the interdiff's files are files the interdiff does not let you
  comment on or mark, since its lines are between two revisions, with none to anchor a thread to.
- **Actions.** An action has a title for menus and hints, a longer help where the title is not
  enough, a check of whether it applies to a subject now, such as only to your own comments, and
  what it runs. It has no name: kinds, places, and keys hold the action itself, so two actions are
  never confused and nothing is looked up by a string. Kinds that do the same thing share it:
  every thread replies the same way, through one action listed on each, and sorts of subject of
  different types share behavior through a generic constructor over what they have in common.
- **What a subject carries.** A subject carries the subjects it is part of, through typed
  projections: a line its file, a file of a commit its commit, a review its summary. Its kind
  reaches theirs, so a line's menu offers its file's and its commit's actions, and a key a line
  does not take reaches its file. The change is about everything on screen, so every key reaches
  it, after the pane, and only the overview, the change's own page, carries it into its menu.
- **Places.** A subject is always found somewhere: a place. Places are finer than panes, since one
  pane holds several, and come in two sorts:
  - **A list** is a place you navigate through, whose entries are its subjects: the sidebar's
    commit list, the sidebar's file list, the lines and file headers of a diff, the overview's
    threads and reviews, a picker's rows. The cursor moves from entry to entry.
  - **A mention** is a place where a subject shows up inside something else, and is not navigated
    through: the sha in a thread's location, the author in a comment's header. The cursor is on the
    thread or the commit, never on the mention.

  The same subject can be in both: a commit is an entry in the sidebar's list and a mention in a
  thread's location, with the same actions in each. A place has actions and keys of its own, for
  what its subjects do there: a sidebar entry opens on enter, a file header folds, a mention offers
  to open what it names. A list also says what happens when the cursor lands on an entry: a sidebar
  commit shows itself in the main pane.
- **Pane focus and the cursor.** Pane focus is which pane gets keys: the sidebar or the main pane.
  The cursor is the entry keys act on within a list: the sidebar's selected commit, the diff line
  under the cursor. Only a list's entries hold the cursor; a mention never does.
- **Keys are bound where their subject is.** A key is bound on a kind, for wherever its subjects
  are, or on a place, for its subjects there. A key press goes to the cursor's subject in its
  place, then to its kind, then to what it carries, then to the pane focus is in, the change, and
  gr itself, and runs the first binding of the key whose action applies. So a subject that takes a
  key keeps it from those after it: a line takes `c`, so on a line `C` reaches its file, and a file
  takes both, so its commit's are passed over. Panes are kinds too, each with moves of its own
  from the same keys, through one helper, and so are modes, such as the comment box and the verdict
  prompt. A key that only says why it does nothing there, as `c` in the interdiff, is bound quietly,
  shown in no menu, hint, or help, so the key does not reach what the subject carries. Bindings are
  written beside the kinds and places, so what a key does on a subject reads where the subject is.
  Remapping keys from the config file will need a stable name for each action, which actions get
  then, for the config alone.
- **Clicks follow from places.** A left click on an entry moves the cursor there, so what the
  list does when the cursor lands runs as if the keyboard had moved it, and pane focus moves to the
  click's pane. A left click on a mention, which cannot hold the cursor, opens its menu. A right
  click opens the menu of whatever is under it, and moves neither the cursor nor the selection. The
  wheel scrolls without moving the cursor.

  | Place | Subject | On cursor | Its keys | A key passes to | Left click | Right click |
  | --- | --- | --- | --- | --- | --- | --- |
  | Sidebar commit list | commit | show it | `enter` open, `c` comment, `m` reviewed | the change | cursor | menu |
  | Sidebar file list | file | jump to it | `enter` open, `c` comment, `m` reviewed | its commit | cursor | menu |
  | Diff | line | | `c` comment, `v` select | its file, its commit | cursor | menu |
  | Diff | file header | | `enter` fold, `c` comment, `m` reviewed | its commit | cursor | menu |
  | Diff | fold | | `enter` expand | its file | cursor | menu |
  | Overview | thread | | `enter` expand, `r` `x` `a` `e` `X`, `o` `O` | the overview | cursor | menu |
  | Overview | review bar | | `enter` the summary, `D` delete | its summary, the overview | cursor | menu |
  | Thread title | sha | | | | menu | menu |
  | Picker | branch | | `enter` pick | | cursor, then pick | |

  A place can override a click, as the picker does, where choosing on a click is expected; the
  override is written where the place is registered, so the defaults stay the rule.
- **One package.** The registry's machinery is the `interact` package, beside `look`: how the TUI
  is acted on, as `look` is how it is drawn. The TUI's own kinds, actions, places, and keys are
  registered in one file of the TUI.
- **Places are types.** A place is a typed value, made only by the registry, and holds subjects of
  one kind: the sidebar's commit list is a list of commits, and a thread's location a mention of
  one. A subject is used in a place by asking that place for it, so a commit cannot be put in a
  place of files, and a subject cannot appear in a place that was never registered: the compiler
  refuses both. Roughly:

  ```go
  // An action is typed by the subject it takes, so it can only be given that subject.
  type Action[M, S any] struct {
  	Title   string
  	Applies func(M, S) bool
  	Run     func(M, S) tea.Cmd
  }

  var markCommit = &Action[*model, commitSubject]{Title: "mark reviewed", Run: ...}

  // A kind's actions, and keys bound to them. Bind takes only an action the kind lists, of its
  // own S, so binding an action on files here does not compile.
  commitKind = interact.NewKind(commitLabel, markCommit, commentOnCommit)
  commitKind.Bind(markCommit, "m/M", "mark the commit reviewed", "m", "M")

  // What a subject carries, through a projection to a T for a kind of T.
  interact.Via(lineKind, func(r rowSubject) (fileSubject, bool) { return r.file, r.hasFile }, fileKind)

  // Places, made only in the registry, with keys of their own.
  sidebarCommits = interact.List(commitKind, interact.OnCursor(showCommit))
  sidebarCommits.Bind(openCommit, "enter/l", "open", "enter", "l", "right")
  ```

  Keys and menus run actions through these typed values; the registry erases the types only
  inside a target, to list a subject's actions and keys together.
- **Help comes from the keys.** The hint bar shows the keys that reach something on the cursor's
  subject and the change, each with the title of what it runs there, so `C` on a line reads
  "comment on file". Help lists the keys of every kind and place, in sections of the kinds and
  places they belong to, where the cursor is first; a test fails on a kind or place whose keys no
  section shows. Neither is written by hand, so neither can drift from what the keys do.
- **Built on bubbles' key and help.** A binding is a `key.Binding` from bubbles, matched with
  `key.Matches`, and the hint bar is bubbles' `help` short view, which already fits it to the
  width. Bubbles' full help view has no section titles, so the help screen keeps its own layout.
- **Menus group actions by subject.** A menu lists the actions of the subject it was opened on,
  then those of the subjects it carries, each under a heading naming that subject, as "Line",
  "File main.go", and "Commit b194130". So an action keeps a short title, such as "mark reviewed",
  and is never ambiguous: the heading says what it acts on. A menu of one subject has no heading,
  and an entry shows the key that runs it there. Where actions are listed flat, as in the palette or
  a filtered menu, an entry is the subject and the title together, as "File main.go: mark
  reviewed", made from the same parts rather than a second title.
- **Targets on screen.** A target is a subject in its place, drawn: drawing a mention through
  `look` also marks where it is on screen, so a click lands on its subject and place. Menus use the
  picker from the base branch popup. Marking is `look`'s: a marked span is wrapped in zero-width
  escapes, and the final frame is scanned for them, so marks survive the composition of panes and
  popups. Bubblezone works the same way, but records what it scans on a background goroutine, so
  a click right after a frame, or a test, can miss the zone; `look`'s scan is synchronous.
- **Controls.** A control is an action drawn as a button, as GitHub's comment box has Comment and
  Cancel. It is drawn from a target, as a key runs on one: the action is found among the target's
  keys and menu, so a click runs what the key runs there, the button shows that key, and it is not
  drawn while the action does not apply. A control is marked with where it is, such as its row,
  and its action's address, never its turn in a frame, since a row's drawing is kept across frames.
  It runs in the mode it was drawn for: a left click on a row's runs it only with no mode open, so
  the rows behind the comment box look the same and do nothing. What shows a state, as a file's
  Viewed box or a fold's Expand, is always drawn; the rest where the cursor or the mouse is, and
  the `+` that comments on a line, in place of its sign, only under the mouse, as GitHub's. gr asks
  the terminal for every mouse motion to know what is under it. Controls are drawn after what they
  act on rather than at the far right, where they are easy to miss. Around the content, as the
  comment box, the prompts, and the title bar, a control is a chip, its label and key on a
  background of its own. Inside the diff and the overview a chip would compete with the code, so
  controls there are quiet: muted text without a key, as `Reply` in a thread's border, a `[ ]
  Viewed` box, or a `v` chevron that folds a file, underlined with their key where the cursor or
  the mouse is. The `+` under the mouse is a chip, as there is one at a time. A hint, which is
  clickable too, underlines its key. A control's label
  is the drawing's, short as a button is, and its title stays the menu's. A control never adds a
  line, since rows are measured once: a thread's buttons sit in its box's bottom border.
- **A command palette.** `ctrl+p` opens the picker on every action that applies to the cursor's
  subjects, by title, filtered as you type. It reaches what the keyboard cannot select,
  such as the commit a thread is on, and needs nothing beyond the registry and the picker.
- **Scope of the first step.** The registry, subjects, places, keys, and generated help come
  first, moving every key over. Mentions are marked from the start, since marking is in `look`
  and costs one wrapper per value. The mouse then acts on them: left clicks move the cursor or
  open a mention's menu, and right clicks open menus. Dragging, hover, and selection by mouse stay
  out.

## Consequences

- An action is written once and works from every place its subject appears, by key, menu, or
  palette.
- What a key does on a subject is written beside the subject's kind or place, and the order a key
  passes through what a subject carries is the one rule to know.
- Adding mouse support for something means drawing it as a subject, or its actions as controls,
  not writing a click handler.
- What differs by where a subject is, as opening a sidebar entry, is declared on its place; what
  differs by state, in an action's check; what a thing is, by its kind.
- Drawing a value somewhere new without registering that place does not compile, so every target
  on screen has its actions and click defined.
- Kinds and places are made when gr starts, after their actions, since actions run code that reads
  them.
- Zone marks are escape sequences inside the rendered text until the final frame is scanned.
  Cutting, padding, and restyling must keep them whole, and the golden screens are taken after the
  scan, so they record what the terminal gets.
- Moving the existing keys over is a refactor of the key handlers, help, and the hint bar. The
  golden screens and key tests guard it, as they did ADR 8's.
