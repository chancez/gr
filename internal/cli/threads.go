package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

func (a *app) threadsCommand() *cobra.Command {
	var cf changeFlags
	var commit string
	var states []string
	var unresolved, outdated, current bool
	cmd := &cobra.Command{
		Use:   "threads [target]",
		Short: "List threads on a change, or on a commit across changes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			var f review.ThreadFilter
			for _, s := range states {
				f.States = append(f.States, store.ThreadState(s))
			}
			if unresolved {
				f.States = []store.ThreadState{store.ThreadOpen, store.ThreadAddressed}
			}
			if cmd.Flags().Changed("outdated") || current {
				f.Outdated = new(outdated && !current)
			}
			var svc *review.Service
			var threads []review.ThreadView
			var err error
			if commit != "" {
				if cf.change != "" || len(args) > 0 {
					return fmt.Errorf("pass --commit or a change, not both")
				}
				if svc, err = a.service(ctx); err != nil {
					return err
				}
				threads, err = svc.CommitThreads(ctx, commit, f)
			} else {
				var change store.Change
				if svc, change, _, err = a.resolveChange(ctx, cf, firstArg(args)); err != nil {
					return err
				}
				threads, err = svc.Threads(ctx, change, f)
			}
			if err != nil {
				return err
			}
			return a.printThreads(ctx, svc, threads)
		},
	}
	cf.register(cmd)
	cmd.Flags().StringVar(&commit, "commit", "", "list threads anchored to this commit, in any change")
	cmd.Flags().StringSliceVar(&states, "state", nil, "only threads in these states: open, addressed, resolved")
	cmd.Flags().BoolVarP(&unresolved, "unresolved", "u", false, "only open and addressed threads")
	cmd.Flags().BoolVar(&outdated, "outdated", false, "only outdated threads (--outdated=false for current ones)")
	cmd.Flags().BoolVar(&current, "current", false, "only threads that are not outdated")
	return cmd
}

func (a *app) threadCommand() *cobra.Command {
	var cf changeFlags
	cmd := &cobra.Command{
		Use:   "thread <ref>",
		Short: "Show one thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, change, _, err := a.resolveChange(ctx, cf, "")
			if err != nil {
				return err
			}
			t, err := svc.ThreadByRef(ctx, change, args[0])
			if err != nil {
				return err
			}
			if a.json {
				out, err := newLookups(svc).thread(ctx, t)
				if err != nil {
					return err
				}
				return a.printJSON(out)
			}
			return a.printThreads(ctx, svc, []review.ThreadView{t})
		},
	}
	cf.register(cmd)
	return cmd
}

func (a *app) printThreads(ctx context.Context, svc *review.Service, threads []review.ThreadView) error {
	l := newLookups(svc)
	if err := l.loadExcerpts(ctx, threads); err != nil {
		return err
	}
	if a.json {
		out := make([]threadJSON, 0, len(threads))
		for _, t := range threads {
			tj, err := l.thread(ctx, t)
			if err != nil {
				return err
			}
			out = append(out, tj)
		}
		return a.printJSON(out)
	}
	for i, t := range threads {
		if i > 0 {
			fmt.Fprintln(a.stdout)
		}
		draft, err := l.draft(ctx, t.ReviewID)
		if err != nil {
			return err
		}
		started, err := l.reviewByID(ctx, t.ReviewID)
		if err != nil {
			return err
		}
		fmt.Fprintln(a.stdout, threadHeader(t, draft, started.Seq))
		if e, ok, err := l.excerpt(ctx, t); err != nil {
			return err
		} else if ok {
			printExcerpt(a.stdout, e)
		}
		for i, l := range t.Was {
			label := "  was: "
			if i > 0 {
				label = "       "
			}
			fmt.Fprintln(a.stdout, label+l)
		}
		for _, c := range t.Comments {
			fmt.Fprintf(a.stdout, "  %s:\n%s\n", c.Author, indent(c.Body, "    "))
		}
	}
	return nil
}

// printExcerpt prints a thread's excerpt as diff lines: old and new line numbers, a marker, and
// the text.
func printExcerpt(w io.Writer, e review.Excerpt) {
	if e.Hunk != "" {
		fmt.Fprintf(w, "  %s\n", e.Hunk)
	} else {
		fmt.Fprintln(w, "  (from the outdated anchor)")
	}
	width := 1
	for _, l := range e.Lines {
		width = max(width, len(strconv.Itoa(max(l.OldLine, l.NewLine))))
	}
	num := func(n int) string {
		if n == 0 {
			return strings.Repeat(" ", width)
		}
		return fmt.Sprintf("%*d", width, n)
	}
	for _, l := range e.Lines {
		mark := " "
		switch l.Kind {
		case git.LineAdded:
			mark = "+"
		case git.LineDeleted:
			mark = "-"
		}
		fmt.Fprintf(w, "  %s %s %s %s\n", num(l.OldLine), num(l.NewLine), mark, l.Text)
	}
}

func threadHeader(t review.ThreadView, draft bool, reviewSeq int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s", t.Label(), t.State)
	if t.State != store.ThreadOpen && t.StateBy.Name != "" {
		fmt.Fprintf(&b, " by %s", t.StateBy.Name)
	}
	if t.Outdated {
		b.WriteString(", outdated")
	}
	if len(t.Was) > 0 {
		b.WriteString(", lines changed")
	}
	if draft {
		b.WriteString(", draft")
	}
	b.WriteString("] ")
	if t.Summary {
		fmt.Fprintf(&b, "summary of review #%d", reviewSeq)
	} else {
		b.WriteString(describeLocation(t.Location))
	}
	return b.String()
}

func describeLocation(l store.Location) string {
	var parts []string
	switch l.Scope {
	case store.ScopeChange:
		return "change"
	case store.ScopeCommit:
		parts = append(parts, "commit "+short(l.Commit))
	case store.ScopeDiff:
		parts = append(parts, "diff")
	}
	if l.Path != "" {
		loc := l.Path
		if l.StartLine > 0 {
			loc += ":" + strconv.Itoa(l.StartLine)
			if l.EndLine != l.StartLine {
				loc += "-" + strconv.Itoa(l.EndLine)
			}
		}
		if l.Side == anchor.SideOld {
			loc += " (old)"
		}
		parts = append(parts, loc)
	}
	return strings.Join(parts, " ")
}

func (a *app) commentCommand() *cobra.Command {
	var cf changeFlags
	var bf bodyFlags
	var commit, path, lines, side string
	cmd := &cobra.Command{
		Use:   "comment [target]",
		Short: "Start a thread in your draft review",
		Long: `Start a thread in your draft review. Submit the review to publish it.

With --commit the thread is on that commit, and --path and --line narrow it to a file and lines.
The commit message is the path ` + review.CommitMsgPath + `. Without --commit, --path puts the
thread on the change's whole diff. With neither, it is a top-level comment on the change.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, change, _, err := a.resolveChange(ctx, cf, firstArg(args))
			if err != nil {
				return err
			}
			nt := review.NewThread{Commit: commit, Path: path, Side: anchor.Side(side)}
			switch {
			case commit != "":
				nt.Scope = store.ScopeCommit
			case path != "":
				nt.Scope = store.ScopeDiff
			default:
				nt.Scope = store.ScopeChange
			}
			if lines != "" {
				if nt.StartLine, nt.EndLine, err = parseLines(lines); err != nil {
					return err
				}
			}
			if nt.Body, err = bf.read(a, ""); err != nil {
				return err
			}
			t, err := svc.StartThread(ctx, change, nt)
			if err != nil {
				return err
			}
			if a.json {
				out, err := newLookups(svc).thread(ctx, t)
				if err != nil {
					return err
				}
				return a.printJSON(out)
			}
			fmt.Fprintf(a.stderr, "started draft thread #%d on change #%d; submit with gr submit\n", t.Seq, change.Seq)
			return nil
		},
	}
	cf.register(cmd)
	bf.register(cmd)
	cmd.Flags().StringVar(&commit, "commit", "", "commit to comment on")
	cmd.Flags().StringVarP(&path, "path", "p", "", "file to comment on, or "+review.CommitMsgPath)
	cmd.Flags().StringVarP(&lines, "line", "l", "", "line N or range N-M")
	cmd.Flags().StringVar(&side, "side", "new", "side of the diff: old or new")
	return cmd
}

func parseLines(s string) (int, int, error) {
	from, to, isRange := strings.Cut(s, "-")
	start, err := strconv.Atoi(from)
	if err != nil {
		return 0, 0, fmt.Errorf("bad line %q", s)
	}
	end := start
	if isRange {
		if end, err = strconv.Atoi(to); err != nil {
			return 0, 0, fmt.Errorf("bad line range %q", s)
		}
	}
	if start < 1 || end < start {
		return 0, 0, fmt.Errorf("bad line range %q", s)
	}
	return start, end, nil
}

func (a *app) replyCommand() *cobra.Command {
	var cf changeFlags
	var bf bodyFlags
	var draft bool
	cmd := &cobra.Command{
		Use:   "reply <ref>",
		Short: "Reply to a thread",
		Long:  "Reply to a thread. The reply is published immediately unless --draft adds it to your draft review.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, change, _, err := a.resolveChange(ctx, cf, "")
			if err != nil {
				return err
			}
			t, err := svc.ThreadByRef(ctx, change, args[0])
			if err != nil {
				return err
			}
			body, err := bf.read(a, "")
			if err != nil {
				return err
			}
			if _, err := svc.Reply(ctx, change, t.ID, body, draft); err != nil {
				return err
			}
			if a.json {
				updated, err := svc.ThreadByRef(ctx, change, t.ID)
				if err != nil {
					return err
				}
				out, err := newLookups(svc).thread(ctx, updated)
				if err != nil {
					return err
				}
				return a.printJSON(out)
			}
			return nil
		},
	}
	cf.register(cmd)
	bf.register(cmd)
	cmd.Flags().BoolVar(&draft, "draft", false, "add the reply to your draft review instead of publishing it")
	return cmd
}

func (a *app) stateCommand(use string, state store.ThreadState, short string) *cobra.Command {
	var cf changeFlags
	var bf bodyFlags
	cmd := &cobra.Command{
		Use:   use + " <ref>...",
		Short: short,
		Long:  short + ". With -m, also reply.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, change, _, err := a.resolveChange(ctx, cf, "")
			if err != nil {
				return err
			}
			for _, ref := range args {
				t, err := svc.ThreadByRef(ctx, change, ref)
				if err != nil {
					return err
				}
				if bf.given() {
					body, err := bf.read(a, "")
					if err != nil {
						return err
					}
					if _, err := svc.Reply(ctx, change, t.ID, body, false); err != nil {
						return err
					}
				}
				if err := svc.SetThreadState(ctx, t.ID, state); err != nil {
					return err
				}
				fmt.Fprintf(a.stderr, "thread %s is %s\n", t.Label(), state)
			}
			return nil
		},
	}
	cf.register(cmd)
	bf.register(cmd)
	return cmd
}

func (a *app) editCommand() *cobra.Command {
	var bf bodyFlags
	cmd := &cobra.Command{
		Use:   "edit <comment-id>",
		Short: "Edit one of your comments",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			c, err := svc.Store().GetComment(ctx, args[0])
			if err != nil {
				return err
			}
			body, err := bf.read(a, c.Body)
			if err != nil {
				return err
			}
			return svc.EditComment(ctx, c.ID, body)
		},
	}
	bf.register(cmd)
	return cmd
}

func (a *app) deleteCommand() *cobra.Command {
	var cf changeFlags
	var thread string
	cmd := &cobra.Command{
		Use:   "delete <comment-id> | --thread <ref>",
		Short: "Delete one of your comments, or a thread you started",
		Long: `Delete one of your comments, or with --thread a thread you started. Deleting a thread's first
comment deletes the thread. A thread with replies from others cannot be deleted, so no one else's
words are lost; edit your comment instead.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if (thread == "") == (len(args) == 0) {
				return fmt.Errorf("pass a comment id or --thread, one of them")
			}
			if thread == "" {
				svc, err := a.service(ctx)
				if err != nil {
					return err
				}
				return svc.DeleteComment(ctx, args[0])
			}
			svc, change, _, err := a.resolveChange(ctx, cf, "")
			if err != nil {
				return err
			}
			t, err := svc.ThreadByRef(ctx, change, thread)
			if err != nil {
				return err
			}
			if err := svc.DeleteThread(ctx, t.ID); err != nil {
				return err
			}
			fmt.Fprintf(a.stderr, "deleted thread %s\n", t.Label())
			return nil
		},
	}
	cf.register(cmd)
	cmd.Flags().StringVar(&thread, "thread", "", "delete this thread, by ref")
	return cmd
}

func (a *app) submitCommand() *cobra.Command {
	var cf changeFlags
	var bf bodyFlags
	var approve, requestChanges bool
	cmd := &cobra.Command{
		Use:   "submit [target]",
		Short: "Submit your draft review",
		Long:  "Submit your draft review with a verdict, comment by default, and an optional summary.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if approve && requestChanges {
				return fmt.Errorf("pass --approve or --request-changes, not both")
			}
			verdict := store.VerdictComment
			if approve {
				verdict = store.VerdictApprove
			} else if requestChanges {
				verdict = store.VerdictRequestChanges
			}
			svc, change, _, err := a.resolveChange(ctx, cf, firstArg(args))
			if err != nil {
				return err
			}
			body := ""
			if bf.given() {
				if body, err = bf.read(a, ""); err != nil {
					return err
				}
			}
			r, err := svc.Submit(ctx, change, verdict, body)
			if err != nil {
				return err
			}
			if a.json {
				out, err := newLookups(svc).review(ctx, r)
				if err != nil {
					return err
				}
				return a.printJSON(out)
			}
			fmt.Fprintf(a.stderr, "submitted review #%d on change #%d: %s\n", r.Seq, change.Seq, r.Verdict)
			return nil
		},
	}
	cf.register(cmd)
	bf.register(cmd)
	cmd.Flags().BoolVar(&approve, "approve", false, "approve the change")
	cmd.Flags().BoolVar(&requestChanges, "request-changes", false, "request changes")
	return cmd
}

func (a *app) discardCommand() *cobra.Command {
	var cf changeFlags
	var reviewSeq int
	cmd := &cobra.Command{
		Use:   "discard [target]",
		Short: "Discard your draft review, or with --review delete a review you submitted",
		Long: `Discard your draft review and everything in it. With --review N, delete review N, one you
submitted, with its threads; that is refused when anyone else commented in them.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, change, _, err := a.resolveChange(cmd.Context(), cf, firstArg(args))
			if err != nil {
				return err
			}
			if reviewSeq > 0 {
				if err := svc.DeleteReview(cmd.Context(), change, reviewSeq); err != nil {
					return err
				}
				fmt.Fprintf(a.stderr, "deleted review #%d\n", reviewSeq)
				return nil
			}
			return svc.Discard(cmd.Context(), change)
		},
	}
	cf.register(cmd)
	cmd.Flags().IntVar(&reviewSeq, "review", 0, "delete this submitted review of yours, by number")
	return cmd
}
