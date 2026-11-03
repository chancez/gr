# gr design

gr is a local, GitHub-style code review tool for git. The TUI is where people review. The CLI is
for agents and editor integrations. State lives in SQLite.

Terms are defined in [GLOSSARY.md](../GLOSSARY.md). Decisions and their reasons are in
[adr/](adr/).

## Goals

- Review a branch, a commit range, or a single commit.
- Comment at three levels: the whole change, a commit, or the change's diff. Line comments take a
  single line or a range, on either side of the diff.
- Review commit messages as well as code.
- Batch comments into a review with a summary and a verdict, like GitHub.
- Track revisions, and show what changed since the last review.
- Keep threads attached to the right code across rebases where possible, and mark them outdated
  otherwise.
- Let agents read reviews, reply, mark threads addressed, and write their own reviews, all through
  the CLI.

## Non-goals for the MVP

- Reviewing uncommitted changes.
- Syncing or pushing reviews anywhere. GitHub export may come later, so the model stays close to
  GitHub's.
- Key remapping and colors in the config file. Planned, see below.
- Neovim integration and quickfix output.
- Shallow clones. gr refuses them, because the root commit is not available.

## Workflows

**Human reviews agent output.** An agent finishes work on a branch. The human runs `gr`, which
finds or creates the change for the current branch, reviews in the TUI, and submits. The agent runs
`gr threads --unresolved --json`, makes fixes, replies, and marks threads addressed. The human sees
a new revision, reads the interdiff, and resolves threads or replies.

**Multiple reviewers.** A human and one or more agents each draft a review on the same change. Each
is submitted independently. The author processes all submitted reviews.

**Agent reviews.** Same as a human, through the CLI: start a review, add threads, submit.

## Domain model

```
Repo 1--* Change 1--* Revision
            1--* Review 1--* Comment
            1--* Thread 1--* Comment
```

- A change belongs to a repo. A branch change records the branch name and base branch name. A
  frozen change records its fixed shas.
- A revision records `base_sha` and `head_sha`. Base is `merge-base(head, base_branch)`.
- A thread belongs to a change and records the revision it was created on and its current anchor.
- A comment belongs to a thread and to the review it was written in. Thread-starting comments are
  always part of a review. Replies may be part of a review or published directly.
- Either the change author or a reviewer may resolve a thread. Who resolved it is recorded.

### IDs

Every row has a UUIDv7 primary key. Human-facing sequence numbers are scoped where they make
sense: revision numbers per change, thread numbers per change. The CLI accepts either a sequence
number or a UUID prefix.

### Change lifecycle

- **open**: the default.
- **merged**: detected when the head becomes reachable from the base branch.
- **abandoned**: set by hand.

## Revisions

gr does not run hooks or a daemon. Each time the CLI or TUI touches a branch change, it resolves the
branch and base. If either sha moved, it records a new revision and re-anchors open threads.

The interdiff between revisions is computed like `git range-diff`: changes that came only from a
moved base are excluded.

## Anchors

An anchor records:

- scope: a commit sha, or the change diff (`base_sha..head_sha` of a revision)
- path, relative to the repo root, or `/COMMIT_MSG`
- side: old or new
- start and end line
- the blob sha of the file on that side
- the anchored lines, plus a few lines of context on each side

Content plus context is what makes re-anchoring possible without tracking commit identity across
rewrites.

## Re-anchoring

When a new revision is recorded, each unresolved thread anchored to the previous revision is
processed in order:

1. If the anchor commit is still in the range, keep it.
2. Otherwise, find the first commit in the new range whose diff for that path contains the anchored
   lines with matching context. Re-anchor there.
3. Otherwise, if the change diff contains them, re-anchor to the change diff.
4. Otherwise, mark the thread outdated. It keeps its original anchor for display.

Threads on `/COMMIT_MSG` re-anchor to the commit with the same subject line, or become outdated.

Git runs before the write transaction, so other writers do not wait on it. The transaction checks
that the latest revision and the threads are as they were when git ran, and starts over if not.

Expect most threads to become outdated after a heavy rebase. That is acceptable: outdated threads
are listed for re-review, as on GitHub.

## Comments

Bodies are Markdown, rendered in the TUI. Suggestion blocks use GitHub's syntax so they can be
applied locally and exported later.

## TUI

- Built on Bubble Tea and the Charm ecosystem: Bubbles, Lip Gloss, Glamour. Chroma for syntax
  highlighting.
- Navigation like less and vim: arrows, `j`/`k`, `g`/`G`, `/` search, `n`/`N`.
- Unified and side-by-side diff, toggleable.
- Mouse support for scrolling, selecting lines, and opening threads.
- Live refresh: polls `PRAGMA data_version` and reloads when another process writes.
- Key bindings live in one keymap so they can be remapped once config exists.

## CLI

- Human-readable output by default, `--json` on every command that prints data.
- The JSON shape is a contract. Changes to it are additive, and breaking ones need a new version.
- Lists threads for a commit, a range, or a change, including threads anchored inside a change that
  covers the commit.

## Later

- More in the config file (`$XDG_CONFIG_HOME/gr/config.toml`, which holds the TUI's settings now): key
  bindings, colors, who may resolve.
- Neovim plugin, likely backed by a `gr serve --stdio` JSON-RPC mode.
- GitHub export of changes, reviews, and threads.
- Reviewing uncommitted changes, by snapshotting the working tree into a commit.
