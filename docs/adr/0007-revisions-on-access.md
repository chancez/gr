# 7. Record revisions on access, without hooks or a daemon

Status: accepted

## Context

Branch changes need a new revision when the head or base moves. Hooks need per-clone setup, and a
daemon is another process to manage.

## Decision

Whenever the CLI or TUI touches a branch change, resolve its branch and base. If either sha moved,
record a new revision and re-anchor threads in the same transaction. Re-anchoring's git work runs
before the transaction, which redoes it if another writer moved the revision or threads meanwhile.

## Consequences

- No setup in any clone.
- A branch rewritten several times between uses records one revision, not several.
- Changes are created implicitly: `gr review [target]` finds or creates one.
