# TODO

Sections are in priority order, and so are the items within them.

## Now: the review loop between a human and an agent

1. Conversation view like GitHub's: every review, thread, and reply in order, each thread shown with
   only the hunk or context around its anchor rather than whole files. Filter by state, such as open
   or addressed, so checking what an agent addressed is one screen. Resolve and reply from there.
2. Thread context for agents: `gr thread N --json` and `gr threads --json` include the hunk around
   the anchor, from the same code as the conversation view.
3. Reply to a review summary. Today only a change-level comment can answer one, and that needs its
   own submitted review. Either publish change-level comments directly, like replies, or treat the
   summary as a thread.
4. Agent skill in `skills/gr`, built with the skill-creator skill: find the change, read open
   threads, fix, reply, mark addressed, and review with comments and a verdict.

## Next: friction in daily use

5. Track commits across rebases, for example from `post-rewrite` or by patch id, so threads and
   reviewed marks survive squashing fixups.
6. Comment on lines outside diff hunks, on context opened from a fold.
7. Load commit diffs lazily. Every diff is loaded up front, so large branches are slow to open.
8. Reviewed marks from the CLI, for agents that review.
9. Test mouse handling in a real terminal.

## Config

10. TOML config in `$XDG_CONFIG_HOME/gr/config.toml`.
11. Remappable keys. Bindings already live in one keymap in `internal/tui/keys.go`.
12. Configurable colors. The light and dark palettes are hard-coded in `internal/tui/styles.go`.
13. Configurable sidebar marks, so icons such as checkboxes can replace the ASCII defaults.
14. Configurable scrolloff and default sidebar width.

## Polish

15. Highlight syntax across lines. Lines are highlighted one at a time, so a block comment spanning
    lines is colored only on its first line.
16. Run git outside the write transaction when re-anchoring, so other writers do not wait on it.
17. Detect squash merges as merged.

## Later

- Neovim integration, likely through `gr serve --stdio`.
- Export reviews to GitHub.
- Review uncommitted changes.
- Shallow clones.
