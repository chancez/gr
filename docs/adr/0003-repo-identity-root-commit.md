# 3. Identify a repo by its root commit

Status: accepted

## Context

Reviews must be found from any worktree, any clone, and after moving the repo. Paths and remote URLs
change. Worktree paths are irrelevant.

## Decision

A repo's identity is its root commit sha, from `git rev-list --max-parents=0 HEAD`. If there are
several roots, use the oldest. Record the last seen path as a hint only. Refuse shallow clones.

## Consequences

- No import, export, or relinking when a repo moves.
- Forks sharing a root share an identity. Harmless, since anchors are shas.
- Empty repos have no identity, and nothing to review.
- Shallow clones are unsupported until a fallback, such as the remote URL, is added.
