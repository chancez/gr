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

**Suggestion**: a fenced ```` ```suggestion ```` block in a comment, holding replacement text for
the anchored lines. Same syntax as GitHub.
