# 2. Shell out to git

Status: accepted

## Context

go-git has incomplete diff support and ignores user configuration such as the diff algorithm,
rename detection, and `diff.orderFile`.

## Decision

Run the `git` binary for all git access, with `--no-ext-diff --no-color` and machine-readable
formats. Parse unified diffs in gr.

## Consequences

- Diffs match what the user sees from `git diff`.
- Requires git on PATH.
- One process per git call. Batch calls where it matters.
