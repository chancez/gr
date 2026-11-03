---
name: gr
description: "Check and address review feedback in gr, the local git review tool: read the reviewer's open threads on your work, fix what they ask, reply, and mark threads addressed. Use when asked to check for, address, or respond to review comments or feedback, or when a gr review or thread is mentioned. To review someone else's change, use gr-review instead."
---

# gr: address review feedback

The reviewer's open threads are your to-do list. **Addressed** is your hand-back: it tells the
reviewer you think a thread is handled, and they **resolve** it or reopen it.

## Before any command

- **Identify yourself.** Set `GR_AUTHOR=Claude` on every command, for example
  `GR_AUTHOR=Claude gr threads`, so your replies read as yours, not the human's or the reviewer's.
  Without it gr would write as the human's git identity, and refuses to under an agent.
- **Read with `--json`.** Threads carry their location and a `context` excerpt: the diff lines the
  comment is about.
- **Pass bodies with `-m` or `-F -`** (stdin). Without either, gr wants an editor on a terminal and
  fails without one.
- **Pick the change.** Run gr from the branch under review and it uses that branch's change. From
  anywhere else, `gr change list` lists the changes and `--change N` selects one. If gr asks for a
  base branch, pass `--base` once; the change remembers it.

Flags and their meanings are in `gr <command> --help`.

## Steps

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

## Resolving

Resolving is the reviewer's decision, so threads you address stay addressed. When the human tells
you to resolve threads, for example after their final pass, resolve the ones they name, or all
addressed threads if they say so: `gr resolve <ref>...`. List them first with
`gr threads --state addressed --json` so you resolve what they reviewed.
