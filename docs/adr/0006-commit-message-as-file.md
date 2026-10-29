# 6. Commit messages are a virtual file

Status: accepted

## Context

Commit messages need line comments, like code.

## Decision

Present each commit message as a file named `/COMMIT_MSG`, as Gerrit does. Anchoring, rendering,
and the CLI treat it like any other file. Re-anchoring matches commits by subject line.

## Consequences

- No separate model for message comments.
- `/COMMIT_MSG` cannot collide with a real path, since repo paths have no leading slash.
