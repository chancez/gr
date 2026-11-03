---
name: gr-review
description: "Review a branch, commit range, or commit with gr, the local git review tool: comment on lines, commits, and commit messages, then submit a verdict. Use when asked to review a change, branch, or commits with gr, or to leave review comments in gr. Instructions after the skill name narrow what to look at or for."
---

# gr-review: review a change

A review is a batch of comments, each starting a thread on the code it concerns, submitted with a
verdict. Comments are drafts until you submit.

## Before any command

- **Identify yourself.** Set `GR_AUTHOR="Claude Reviewer"` on every command, for example
  `GR_AUTHOR="Claude Reviewer" gr comment ...`, so your review reads as a reviewer's, not the
  human's, nor the agent's who addresses it. Without it gr would write as the human's git identity,
  and refuses to under an agent.
- **Pass bodies with `-m` or `-F -`** (stdin). Without either, gr wants an editor on a terminal and
  fails without one.

Flags and their meanings are in `gr <command> --help`.

## Steps

1. **Open the change**: `gr review <target>` finds or creates it, where the target is a branch
   (default the current one), `A..B`, or a commit. If gr asks for a base branch, pass `--base`.
   Read the commits with `git log` and `git show`. When the human gave instructions with the skill,
   such as an area to focus on, they set the scope.
2. **Comment** where each problem is:
   - lines in a commit: `gr comment --commit <sha> -p <path> -l 10-12 -m "..."`
   - the commit message: `-p /COMMIT_MSG` with the message's line numbers
   - the whole commit: `--commit <sha>` alone
   - lines of the change's whole diff: `-p <path> -l <lines>` without `--commit`
   - the change as a whole: neither

   Lines must be inside the diff: changed lines or the context lines around them. Use
   `--side old` for deleted lines. gr says so when a line is outside the diff.

   After reading a commit, mark it reviewed with `gr mark --commit <sha>`. `gr change show` lists
   what you marked, so a long review can resume where it stopped.
3. **Submit**: `gr submit --request-changes -m "<summary>"`, or `--approve`, or neither for a plain
   comment. The summary becomes a thread the author can reply to.

**Done** when every problem you found has a comment on the code it concerns and the review is
submitted with a verdict that matches them.
