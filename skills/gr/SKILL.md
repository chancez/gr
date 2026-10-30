---
name: gr
description: "Work with local code reviews in gr, the git review tool: read a reviewer's threads on your work, fix what they ask, reply, and mark threads addressed; or review a branch, commit range, or commit by leaving line and commit comments and submitting a verdict. Use when asked to address, respond to, or check review feedback or comments, when a gr review or thread is mentioned, or when asked to review a branch, commits, or a change with gr."
---

# gr

gr keeps GitHub-style reviews of local git changes in a SQLite database: a **change** (a branch,
range, or commit under review) has **revisions** (snapshots as the branch moves), **reviews** (one
author's batch of comments with a verdict), and **threads** (a conversation on the change, a commit,
a file, lines, or the commit message). A human usually reviews in gr's TUI; you work through the
CLI.

## Before any command

- **Identify yourself.** Set `GR_AUTHOR` to your name on every command, for example
  `GR_AUTHOR=Claude gr threads`. Without it gr writes as the human's git identity, and your replies
  read as theirs.
- **Read with `--json`.** Every data command takes it. Threads carry their location and a
  `context` excerpt: the diff lines the comment is about.
- **Pass bodies with `-m` or `-F -`** (stdin). Without either, gr wants an editor on a terminal and
  fails without one.
- **Find the change.** On the branch under review, commands find its change on their own. Elsewhere,
  `gr changes` lists them and `--change N` picks one. If gr asks for a base branch, pass `--base` once;
  the change remembers it.

Flags and their meanings are in `gr <command> --help`; check there rather than guessing.

## Addressing review feedback

The reviewer's open threads are your to-do list. **Addressed** is your hand-back: it tells the
reviewer you think a thread is handled, and they **resolve** it or reopen it. Resolving is the
reviewer's call, so leave it to them.

1. **Read the open threads**: `gr threads --state open --json`. Use `--state open` rather than
   `--unresolved`, which also lists the threads you already addressed. A review's summary is a
   thread too, with a ref like `r1`; its asks count the same as any line comment.
2. **Understand each one** from its `context`, its `location` (commit, path, lines), and every
   comment in it, not only the first: later replies may have narrowed the ask. A thread marked
   `outdated` is on code that has since changed; check the current code before acting on it.
3. **Fix and commit.** Make the change the thread asks for, commit it, and note the short sha.
   Group related threads into one commit when the fix is shared.
4. **Reply and hand back** each thread with its `ref`:
   `gr address <ref> -m "Fixed in <sha>: <what changed and where>."` One reply per thread, saying
   what changed so the reviewer can check it without rereading the diff.
5. **When you will not, or cannot, do what a thread asks**, reply with your reason or question
   using `gr reply <ref> -m "..."` and leave it open. Open means the reviewer still owes a decision.

**Done** when `gr threads --state open --json` lists only threads you replied to with a question or
a reason for not acting, and every fix is committed.

A thread the reviewer reopened is a fix they did not accept: read their newest comment and treat it
as a fresh ask.

## Reviewing a change

1. **Open the change**: `gr review <target>` finds or creates it, where the target is a branch (default
   the current one), `A..B`, or a commit. Read its commits with `git log` and `git show`.
2. **Comment** where the problem is. Each comment starts a thread in your draft review:
   - lines in a commit: `gr comment --commit <sha> -p <path> -l 10-12 -m "..."`
   - the commit message: `-p /COMMIT_MSG` with the message's line numbers
   - the whole commit: `--commit <sha>` alone
   - lines of the change's whole diff: `-p <path> -l <lines>` without `--commit`
   - the change as a whole: neither

   Lines must be inside the diff: changed lines or the context lines around them. Use
   `--side old` for deleted lines. gr says so when a line is outside the diff.
3. **Submit**: `gr submit --request-changes -m "<summary>"`, or `--approve`, or neither for a plain
   comment. Nothing is visible to anyone until you submit; the summary becomes a thread the author
   can reply to.

**Done** when every problem you found has a comment on the code it concerns and the review is
submitted with a verdict that matches them.
