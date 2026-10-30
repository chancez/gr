# gr

Local, GitHub-style code review for git. Review in the TUI. Agents and editors use the CLI.

Reviews live in one SQLite database (`$XDG_DATA_HOME/gr/gr.db`), keyed by each repo's root commit,
so every clone and worktree of a repo sees the same reviews.

## Install

```
mise install
mise run install    # -> ~/.local/bin/gr
```

## Review in the TUI

```
gr                  # the current branch against the default branch
gr feature          # a branch
gr main..feature    # a fixed range
gr abc123           # one commit
```

The sidebar has the overview, the whole diff, each commit, and, after the branch moves, the changes
since your last review. `?` lists every key. The most used:

| Key | Action |
| --- | --- |
| `j` `k` `d` `u` `g` `G` | move |
| `tab` | switch between sidebar and diff |
| `c` | comment on the line, selection, file, or change |
| `v` | start or end a line selection |
| `C` | comment on the whole file or commit |
| `r` `x` `a` | reply, resolve or reopen, mark addressed |
| `S` | submit your review: approve, request changes, or comment |
| `s` | toggle split diff |

Comments are Markdown. `ctrl+e` writes one in `$EDITOR`.

## For agents

Every data command takes `--json`. Set `GR_AUTHOR` to your agent's name.

```
gr threads --unresolved --json             # what the reviewer asked for
gr reply 3 -m "Fixed in the next commit."
gr address 3                               # for the reviewer to confirm and resolve
gr threads --commit HEAD --json            # threads on one commit, in any change
```

Reviewing works the same way:

```
gr comment --commit HEAD -p main.go -l 10-12 -m "This can be nil."
gr comment --commit HEAD -p /COMMIT_MSG -l 1 -m "Subject is too long."
gr comment -p main.go -l 4 -m "On the whole diff."
gr comment -m "Top-level comment."
gr submit --request-changes -m "Summary."
```

## Docs

- [docs/design.md](docs/design.md): what gr does and why
- [docs/architecture.md](docs/architecture.md): how the code is laid out
- [docs/adr](docs/adr): decisions
- [GLOSSARY.md](GLOSSARY.md): terms
