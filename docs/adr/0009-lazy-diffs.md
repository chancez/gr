# 9. Load diffs lazily into a bounded cache

Status: accepted

## Context

The TUI loads every commit's diff when it opens a change, and again after every action. On this
repository's own branch that is 66 git processes, about half a second, before the screen shows the
result of a keypress. A large change makes it worse in time and in memory, since every hunk of every
commit is held at once.

Most of the UI needs only each commit's file list: file counts, reviewed marks, review progress, and
the sidebar. Diff contents are needed only for the view on screen, an overview excerpt, or placing
threads in the whole diff.

## Decision

- **File lists eagerly, in one call.** `git log --raw` over the change's range gives every commit's
  files, statuses, and blob ids in one process. That is all the sidebar and reviewed marks need.
- **Diff contents lazily.** Hunks load when something needs them: opening a commit or the whole
  diff, an overview excerpt (one file of one commit), or placing threads in the whole diff (when it
  opens).
- **A bounded cache.** Loaded diffs are kept in a least-recently-used cache, keyed by the pair of
  revisions and the path, and bounded by the total number of diff lines rather than entries, so a
  huge commit costs what it weighs. A diff between two fixed revisions never changes, so entries
  never go stale and nothing is invalidated.
- **Reload what changed.** After an action, the TUI reloads threads, reviews, and marks from the
  database, and checks the branch head with one `git rev-parse`. Git work runs only when the head
  has moved.

## Consequences

- An action's result shows as soon as the database read returns.
- Memory stays bounded however large the change, and revisiting a view is free while it is cached.
- Opening a commit for the first time can show a short loading state in the main pane, with the
  rest of the TUI usable.
- Search covers what is loaded. The current view is loaded by being shown, so searching it is
  unaffected.
- Loading becomes asynchronous in more places, which the views must handle: a view can be shown
  before its diff arrives.
