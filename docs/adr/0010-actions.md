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

Each kind of value defines the actions on it, each place says what its subjects do when the cursor
lands on them, is activated, or is asked for a menu, and keys, menus, and the mouse all run
actions through the registry.

- **Subjects.** A subject is a value an action acts on, typed by its kind: change, commit, file,
  hunk, line, selection, thread, comment, review, and branch. A subject carries what acting on it
  needs, such as the commit and path of a file. The kinds are the ones ADR 8 draws, so a value
  that is drawn as a sha is a commit subject wherever it appears.
- **Actions belong to subject kinds.** Each kind defines its actions: a commit can be opened,
  marked reviewed, commented on, or have its sha copied. An action has a name, such as
  `commit.mark-reviewed`, a title for menus and help, a check of whether it applies to a given
  subject, such as only to your own comments, and what it runs. Every action of a kind is
  available wherever a subject of that kind is, so a commit offers the same actions in the sidebar
  as in a thread's location. Every kind also has a menu action, which opens a menu of every action
  of the subject that applies. An action several kinds share by name, as `comment` on a
  selection, a line, a file, a commit, or the change, is one action per kind, so a key finds the
  most specific one the cursor has.
- **Places.** A subject is always found somewhere: a place. Places are finer than panes, since one
  pane holds several, and come in two sorts:
  - **A list** is a place you navigate through, whose entries are its subjects: the sidebar's
    commit list, the sidebar's file list, the lines and file headers of a diff, the overview's
    threads and reviews, a picker's rows. The cursor moves from entry to entry.
  - **A mention** is a place where a subject shows up inside something else, and is not navigated
    through: the sha in a thread's location, the author in a comment's header, the sha in a
    commit's header. The cursor is on the thread or the commit, never on the mention.

  The same subject can be in both: a commit is an entry in the sidebar's list and a mention in a
  thread's location, with the same actions in each.
- **Fewer actions in a place.** Whether an action applies to a subject's state is the action's own
  check: opening a commit does not apply in that commit's own view. Where a place should offer
  fewer of its kind's actions, as a picker offering only to pick, the place names the ones it
  offers, and by default it offers them all.
- **Pane focus and the cursor.** Pane focus is which pane gets keys: the sidebar or the main pane.
  The cursor is the entry keys act on within a list: the sidebar's selected commit, the diff line
  under the cursor. Only a list's entries hold the cursor; a mention never does.
- **Three handlers.** Each subject in a place, a pair registered together, says what it does. A
  list's subjects can have all three; a mention, which never holds the cursor, has only the menu:
  - **on cursor**, when the cursor lands on it, by key or by click. A sidebar commit shows itself
    in the main pane; a diff line does nothing more.
  - **activate**, when it is chosen, by enter or, in the sidebar, l. A sidebar commit shows itself
    and moves pane focus to the main pane; a file header collapses or expands its file.
  - **menu**, its menu action unless the place says otherwise.

  The handlers run actions of the subject's kind. A subject with no on cursor or activate handler
  does nothing for them.
- **Moves belong to places.** j and k, arrows, pages, and jumps such as next hunk or next thread
  move the cursor through a place, and the place runs the on cursor handler of where it lands.
  They are not actions on a subject, but they are bound in the same keymap and shown in help.
- **Other keys act on the cursor's subject.** The cursor's subject carries the rest, most specific
  first: a diff line carries its hunk, its file, the commit, and the change. A key bound to an
  action, as `m` to marking reviewed, runs it on the first of those that has it, so `m` on a line
  marks its file. This replaces the per-pane logic that decides what a key acts on.
- **Clicks follow from the handlers.** A left click on an entry moves the cursor there, so its on
  cursor handler runs as if the keyboard had moved it, and pane focus moves to the click's pane. A
  left click on a mention, which cannot hold the cursor, runs its menu. A right click runs the
  menu of whatever is under it. A double click could activate, as in most interfaces, though
  bubbletea does not report one, so gr would tell it from two clicks in quick succession. The
  wheel scrolls without moving the cursor.

  | Place | Subject | Moves | On cursor | Activate | Keys | Left click | Right click |
  | --- | --- | --- | --- | --- | --- | --- | --- |
  | Sidebar commit list | commit | j k | show it in the main pane | show it, focus the main pane | `m` reviewed, `C` comment | cursor | menu |
  | Sidebar file list | file | j k | jump to it in the main pane | jump, focus the main pane | `m` reviewed, `c` comment | cursor | menu |
  | Diff | line | j k, J K, ] [ | | | `c` comment, `v` select, `m` its file | cursor | menu: line, file, commit |
  | Diff | file header | j k, ] [ | | collapse or expand | `m` reviewed | cursor | menu |
  | Diff | gap | j k | | expand | `zo` | cursor | menu |
  | Overview | thread | j k, t T | | expand or collapse | `r` `x` `a` `o` `O` | cursor | menu |
  | Overview | review bar | j k | | expand the summary | `D` delete | cursor | menu |
  | Thread title | sha | | | | | menu | menu |
  | Picker | branch | up down | | pick it | | cursor, then pick | |

  A place can override a click, as the picker does, where choosing on a click is expected; the
  override is written where the place is registered, so the defaults stay the rule.
- **Places are types.** A place is a typed value, made only by the registry, and holds subjects of
  one kind: the sidebar's commit list is a list of commits, and a thread's location a mention of
  one. A subject is used in a place by asking
  that place for it, so a commit cannot be put in a place of files, and a subject cannot appear in
  a place that was never registered: the compiler refuses both. The component that draws a place,
  such as the sidebar's commit list, is given its place when it is built, so drawing code does not
  choose a place at each call and cannot name the wrong one. Roughly:

  ```go
  // An action is typed by the subject it takes, so it can only be given that subject.
  type Action[S any] struct {
  	Name    string
  	Title   string
  	Applies func(S) bool
  	Run     func(S) tea.Cmd
  }

  var CopySHA = Action[CommitSubject]{Name: "commit.copy-sha", Title: "Copy the sha",
  	Run: func(c CommitSubject) tea.Cmd { return copyText(c.SHA) }}

  // A kind's actions, defined once. kind[S] takes only Action[S], so listing an action on files
  // here does not compile. Commit.Menu is the kind's menu, as an Action[CommitSubject].
  var Commit = kind("commit", OpenCommit, ShowCommit, MarkCommitReviewed, CommentOnCommit, CopySHA)

  // Places, made only in the registry: lists, with their handlers, and mentions. A kind's
  // OnCursor, Activate, and Menu take an Action[S] for its own S, so a list of commits cannot run
  // an action on files.
  var SidebarCommits = Commit.list("sidebar.commits", Commit.OnCursor(ShowCommit), Commit.Activate(OpenCommit))
  var ThreadLocation = Commit.mention("thread.location")

  // In the TUI, the list holds its place, and only the place makes its targets. Target takes the
  // place's S, so a sidebar of commits cannot be handed a file.
  commits := newNavList(act.SidebarCommits)
  line := commits.place.Target(c).Draw(look.SHA(c.SHA).Short())
  ```

  A place whose subject comes with others, as a diff line comes with its hunk, file, and commit,
  holds a subject that carries them all, and its kind reaches theirs through typed projections, so
  the actions of each are available there:

  ```go
  var Line = kind("line", CommentOnLine, SelectLines).
  	With(Via(func(l LineSubject) FileSubject { return l.File }, File)).
  	With(Via(func(l LineSubject) CommitSubject { return l.Commit }, Commit))
  ```

  `Via` takes a function from the line to a `T` and a kind of `T`, so a projection that returns a
  file cannot be paired with the commit kind. Keys and menus run actions through these typed
  values; the registry erases the types only inside itself, to list a subject's actions together.
- **Bindings.** A key is bound to an action in a scope: everywhere, a pane (sidebar, overview,
  diff), or a mode (composing, picking). The innermost scope wins. All default bindings live in one
  keymap table, keyed by action name and scope, and the config file overrides entries in it, so
  keys are redefined in one place.
- **Help comes from actions and bindings.** An action carries its help text, a short form for the
  hint bar and a longer one for help, and a hint priority. Help lists every binding by scope, the
  current scope first; the hint bar shows the highest-priority bindings that apply to the cursor's
  subjects and fit. Neither is written by hand, so neither can drift from what the keys do, and a
  remapped key shows as remapped everywhere.
- **Built on bubbles' key and help.** A binding is a `key.Binding` from bubbles: its keys, its help
  key and description from the action, and enabled only where the action applies to the cursor's
  subjects. Keys are matched with `key.Matches`, remapped with `SetKeys`, and the hint bar is
  bubbles' `help` short view over the enabled bindings, which already fits them to the width.
  Bubbles' full help view has no section titles, so the help screen keeps its own layout, with its
  sections and bindings from the registry.
- **Menus group actions by subject.** A menu lists the actions of the subject it was opened on,
  then those of the subjects it carries, each under a heading naming that subject, as "Line 12",
  "File main.go", and "Commit b194130". So an action keeps a short title, such as "Mark reviewed",
  and is never ambiguous: the heading says what it acts on. A menu of one subject has no heading.
  Where actions are listed flat, as in the palette, an entry is the subject and the title together,
  as "File main.go: mark reviewed", made from the same parts rather than a second title.
- **Targets on screen.** A target is a subject in its place, drawn: drawing its value through
  `look` also marks where it is on screen, so a click lands on its subject and place. Menus use the
  picker from the base branch popup. Marking uses bubblezone (v2, on the same Charm libraries),
  which tags regions as they are drawn and finds them in the final frame, so marks survive the
  composition of panes and popups.
- **A command palette, later.** `ctrl+p` would open the picker on every action that applies to the
  cursor's subjects, by title, filtered as you type. It reaches what the keyboard cannot select,
  such as the commit a thread is on, and needs nothing beyond the registry and the picker, so it
  can come after the first step.
- **Scope of the first step.** The registry, subjects, places, bindings, and generated help come
  first, moving every key over. Targets are marked from the start, since marking is in `look`
  and costs one wrapper per value. The mouse then acts on them through the same handlers: left
  clicks move the cursor or open a mention's menu, and right clicks open menus. Dragging, hover, and
  selection by mouse stay out.

## Consequences

- An action is written once and works from every place its subject appears, by key, menu, or
  palette.
- Keys, help text, and hints are defined in one place, and remapping a key is one config entry.
- Adding mouse support for something means drawing it as a subject, not writing a click handler.
- Each action is defined once, on its kind, and is available wherever its kind is. What differs by
  place, what its handlers run, is declared where the place is, not checked inside actions.
- Drawing a value somewhere new without registering that place does not compile, so every target
  on screen has its actions and click defined.
- Bubblezone's marks are escape sequences inside the rendered text until the final frame is
  scanned. Cutting and padding must keep them whole, and the golden screens are taken after the
  scan, so they record what the terminal gets.
- Moving the existing keys over is a refactor of the key handlers, help, and the hint bar. The
  golden screens and key tests guard it, as they did ADR 8's.
