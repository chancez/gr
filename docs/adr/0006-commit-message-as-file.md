# 6. Commit messages are a virtual file

Status: accepted

## Context

Commit messages need line comments, like code.

## Decision

Treat each commit message as a file named `/COMMIT_MSG`, as Gerrit does. Anchoring and the CLI
treat it like any other file. Re-anchoring matches commits by subject line.

The TUI does not list it among the files: its lines are the commit's header, a box with the subject
and body above a footer of the author, date, size, and sha, as GitHub shows a commit. A message
line takes comments and selections as a file's line does, and its threads sit under it in the box.
The message is reviewed with the commit, not as a file of its own.

## Consequences

- No separate model for message comments.
- The message reads as part of the commit, and stays in view once the commit's files are reviewed
  and folded.
- `/COMMIT_MSG` cannot collide with a real path, since repo paths have no leading slash.
