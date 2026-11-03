# 5. Anchor threads to content, not commit identity

Status: accepted, amended by [ADR 11](0011-rewritten-commits.md), which follows commits across
rewrites

## Context

Rebases and amends change every sha. Tracking commits across rewrites needs Change-Id trailers, jj,
or hooks. None are in use, and losing some threads to a rebase is acceptable.

## Decision

An anchor stores its scope (commit or change diff), path, side, line range, blob sha, and the
anchored lines with surrounding context. On a new revision, re-anchor by matching that content:
same commit, then the first matching commit, then the change diff, else outdated. Commit identity
across rewrites is not tracked.

## Consequences

- Works for any workflow with no setup.
- Heavy rebases make most threads outdated. They are listed for re-review.
- Rewrite tracking, for example via `post-rewrite`, can be added later as an extra matching step.
  ADR 11 adds it, by author and author date.
