# 4. UUIDv7 keys plus scoped sequence numbers

Status: accepted

## Context

Reviews may be exported to GitHub or elsewhere later, which rules out autoincrement keys as
identity. People and agents still need short handles like "thread 12".

## Decision

Every row has a UUIDv7 primary key. Revisions and threads also get a sequence number scoped to
their change. The CLI accepts either form, and UUID prefixes.

## Consequences

- IDs are stable across export and merge.
- UUIDv7 is time-ordered, which keeps SQLite indexes local.
- Sequence numbers are allocated inside the inserting transaction.
