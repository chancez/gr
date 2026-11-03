# 11. Follow commits across rewrites by author and author date

Status: accepted

## Context

A change is reviewed while it is rewritten: fixups are squashed into the commits they fix, commits
are reworded and reordered, and the branch is rebased onto a newer base. Every rewrite gives every
commit a new sha. ADR 5 anchors threads to content, so a thread whose lines a rewrite leaves alone
follows them, but a reviewed mark is keyed by its commit's sha and is lost to any rewrite, and a
thread whose lines a fixup changed goes outdated. That is the thread whose ask was just answered:
the fix is the rewrite.

Knowing which new commit rewrote which old one fixes both. Git does not record it, apart from the
`post-rewrite` hook, which needs installing in every repository and misses rewrites made elsewhere.
Change-Id trailers and jj are not in use.

## Decision

- **A commit's identity is its author and author date.** Rebase, amend, reword, and fixup squashes
  keep both; only the committer and its date change. A commit of an earlier revision is rewritten
  by the commit of the new revision with the same author email and author date. Where several
  share them, as from `git am`, the subject decides, and where that does not, none does.
- **Patch ids say whether a rewrite changed it.** A commit with the same `git patch-id --stable`
  as the one it rewrote has the same diff, as after a clean rebase or a reword.
- **Reviewed marks follow.** A commit marked reviewed is marked on the commit that rewrote it when
  its patch id is unchanged. When it changed, its files whose content is the same in both stay
  marked, so only what the rewrite touched is left to review.
- **Threads follow.** A thread on a rewritten commit is first matched by content in the commit
  that rewrote it, then as before. When its lines changed, it moves to where they are now in that
  commit, mapped through the diff between the two commits, and is marked changed, keeping the lines
  it was on to show beside the new ones. It is outdated only when the commit or its lines are gone.

## Consequences

- No setup, and it works for rewrites made anywhere.
- A commit whose author date was reset, as by `git rebase --reset-author-date` or `git commit
  --amend --reset-author`, is a new commit: its marks are lost and its threads matched by content
  only, as before.
- The old commit must still be in the repository to read its author and patch id. A pruned one is
  treated as gone.
- A `post-rewrite` hook could be added later as an exact first step, with this as the fallback.
