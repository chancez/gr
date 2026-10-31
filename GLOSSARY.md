# Glossary

**Repo**: a git repository, identified by its root commit sha. The same in every clone and worktree,
and unaffected by moving the repository. The last seen path is recorded as a hint, never as identity.

**Change**: the thing under review, like a GitHub pull request. Its target is a branch with a base
branch, a commit range, or a single commit. Lifecycle: open, merged, abandoned.

**Branch change**: a change whose target is a branch. It follows the ref and gains revisions as the
head or base moves.

**Frozen change**: a change whose target is a commit range or a single commit. It has exactly one
revision.

**Revision**: a snapshot of a change: `(base_sha, head_sha)`, numbered from 1. Recorded whenever gr
sees that either sha has moved.

**Interdiff**: the difference between two revisions of a change, excluding changes that only came
from the base branch.

**Review**: one author's batch of threads and comments on a change, with a summary and a verdict.
States: draft, submitted. A change has many reviews.

**Verdict**: what a submitted review concludes: comment, approve, request changes.

**Author**: a human or a named agent. Defaults to `git config user.name`/`user.email`, overridden by
`--author` or `GR_AUTHOR`.

**Thread**: a conversation with an optional anchor. Without an anchor it is a top-level comment on
the change. States: open, addressed, resolved. Outdated is tracked separately.

**Comment**: one message in a thread. Markdown. Draft until its review is submitted, except replies
to existing threads, which may be published directly.

**Anchor**: where a thread points: a commit or the change's whole diff, a path, a side (old or new),
a line range, and the content and context of those lines.

**Commit message file**: the commit message presented as a virtual file named `/COMMIT_MSG`, so it
is anchored and commented on like any other file.

**Re-anchoring**: moving a thread's anchor onto a new revision after a rebase or amend.

**Outdated**: a thread whose anchor could not be re-anchored onto the latest revision.

**Reviewed mark**: an author's note that they reviewed a commit, a file in a commit, or a file of the
change's whole diff. A commit counts as reviewed when it or all of its files are marked. A mark on
the whole diff is tied to the file's content and lapses when it changes.

**Suggestion**: a fenced ```` ```suggestion ```` block in a comment, holding replacement text for
the anchored lines. Same syntax as GitHub.

## Drawing the TUI

**Palette**: the colors available to the TUI, named by how they look, such as `ink` or `lightBlue`.
A palette color has no meaning of its own.

**Role**: what a color is for, such as text, muted, success, danger, attention, done, added, or
deleted. Named by intent rather than hue, so a theme or the config can make danger blue.

**Theme**: an assignment of palette colors to roles, for a light or dark background. Built in:
default and colorblind.

**Element**: a kind of text on screen, such as an author, a sha, or a thread's state, styled from
the roles. Variants cover values, such as `verdict.approve`.

**Prop**: an option a place asks an element for, such as dim or strong.

**Surface**: where a line sits, such as plain, the cursor, a heading bar, or an added line. It owns
the background.

## Acting in the TUI

**Subject**: a value an action acts on, typed by its kind: change, commit, file, hunk, line,
selection, thread, comment, review, or branch.

**Action**: something the TUI does to a subject, such as marking a commit reviewed. Defined once, by
name, on its subject's kind, and available wherever a subject of that kind is.

**Binding**: a key bound to an action in a scope: everywhere, a pane, or a mode.

**Place**: where a subject is found, registered with the subject kind it holds and its handlers. A
subject can be used only in a place registered for its kind. Either a list or a mention.

**List**: a place you navigate through, whose entries are its subjects, such as the sidebar's commit
list or a diff's lines. The cursor moves from entry to entry.

**Cursor**: the entry keys act on within a place, such as the sidebar's selected commit or the diff
line under the cursor. Moved by keys and clicks. Pane focus, which pane gets keys, is separate.

**Handler**: what a subject in a place does: on cursor, when the cursor lands on it; activate, when
it is chosen by enter; and menu. Clicks follow from them.

**Mention**: a place where a subject shows up inside something else and is not navigated through,
such as the sha in a thread's location. It never holds the cursor, and a click on it opens its menu.

**Target**: a spot on screen where a subject is drawn, with its place, which the mouse can act on.
