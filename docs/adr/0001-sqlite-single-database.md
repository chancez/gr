# 1. One SQLite database in XDG data home

Status: accepted

## Context

Review state must be shared by every worktree and clone of a repo, and survive moving the repo.
Storing it inside `.git` would tie it to one clone.

## Decision

One database at `$XDG_DATA_HOME/gr/gr.db` for all repos, keyed by repo identity (ADR 3). Use
`modernc.org/sqlite`, in WAL mode.

## Consequences

- One migration path, and queries across repos for free.
- Pure Go, so no cgo and simple cross-compiling.
- WAL allows the TUI and agents to read and write concurrently.
- Reviews do not travel with a clone. Export is a separate feature.
