# 12. Follow a branch's change through renames, by git's reflog

Status: accepted

## Context

A branch change is found by its branch's name. Renaming the branch, as from `poc-foobar` to
`feature-foobar` once the idea holds up, leaves its change on a name that is gone, and
opening the new name starts a new, empty change. The reviews stay behind.

Git records renames: `git branch -m` writes "renamed refs/heads/old to refs/heads/new" into the
branch's reflog, which moves with the branch, and `git branch -c` writes "copied". The record is
local: it is not pushed or fetched, and expires with the reflog.

## Decision

- **A recorded rename moves the change.** When a branch has no change with reviews of its own and
  its reflog names an earlier name with an open change, and that name is no longer a branch, the
  change moves onto the new name. A change already opened on the new name without reviews is
  dropped. The CLI and the TUI say what moved.
- **A copy is left.** When the earlier name is still a branch, copied or made again since, either
  could be the change's, so gr leaves it and says how to move it.
  Names before a copy in the log are its source's, and are not followed.
- **Anything else is moved by hand**, with `gr change branch <change> [branch]`.

## Consequences

- No setup, and a rename followed by more work, or several renames, is still found.
- A rename made in another clone, a branch recreated under a new name with `git checkout -b`, or
  one renamed longer ago than the reflog keeps, is not found. Matching those by their commits, as
  ADR 11 matches rewritten commits, could be added, asking rather than moving.
- A change with reviews on the new name is never replaced: two changes' reviews are not merged.
