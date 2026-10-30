package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

func (a *app) reviewCommand() *cobra.Command {
	var cf changeFlags
	cmd := &cobra.Command{
		Use:   "review [target]",
		Short: "Find or create the change for a target and show it",
		Long: `Find or create the change for a target, record a new revision if it moved, and show it.

A target is a branch (default: the current one), a range A..B, or a commit.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, change, rev, err := a.resolveChange(cmd.Context(), cf, firstArg(args))
			if err != nil {
				return err
			}
			return a.showChange(cmd.Context(), svc, change, rev)
		},
	}
	cf.register(cmd)
	return cmd
}

func (a *app) changeListCommand() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List changes in this repository",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc, err := a.service(ctx)
			if err != nil {
				return err
			}
			var states []store.ChangeState
			if !all {
				states = []store.ChangeState{store.ChangeOpen}
			}
			changes, err := svc.ListChanges(ctx, states...)
			if err != nil {
				return err
			}
			if a.json {
				out := make([]changeJSON, 0, len(changes))
				for _, c := range changes {
					rev, err := svc.Store().LatestRevision(ctx, c.ID)
					if err != nil {
						return err
					}
					out = append(out, toChange(c, &rev))
				}
				return a.printJSON(out)
			}
			for _, c := range changes {
				fmt.Fprintf(a.stdout, "#%d\t%s\t%s\t%s\n", c.Seq, c.State, describeTarget(c), c.Title)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "include merged and abandoned changes")
	return cmd
}

func (a *app) changeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change",
		Short: "Manage a change",
	}
	var cf changeFlags
	show := &cobra.Command{
		Use:   "show [target]",
		Short: "Show a change with its revisions and reviews",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, change, rev, err := a.resolveChange(cmd.Context(), cf, firstArg(args))
			if err != nil {
				return err
			}
			return a.showChange(cmd.Context(), svc, change, rev)
		},
	}
	cf.register(show)

	setState := func(use, short string, state store.ChangeState) *cobra.Command {
		var cf changeFlags
		c := &cobra.Command{
			Use:   use + " [target]",
			Short: short,
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				svc, change, _, err := a.resolveChange(cmd.Context(), cf, firstArg(args))
				if err != nil {
					return err
				}
				if err := svc.SetChangeState(cmd.Context(), change, state); err != nil {
					return err
				}
				fmt.Fprintf(a.stderr, "change #%d is %s\n", change.Seq, state)
				return nil
			},
		}
		cf.register(c)
		return c
	}

	var tf changeFlags
	title := &cobra.Command{
		Use:   "title <title>",
		Short: "Set a change's title",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, change, _, err := a.resolveChange(cmd.Context(), tf, "")
			if err != nil {
				return err
			}
			return svc.Store().SetChangeTitle(cmd.Context(), change.ID, args[0])
		},
	}
	tf.register(title)

	cmd.AddCommand(a.changeListCommand(), show, title,
		setState("abandon", "Abandon a change", store.ChangeAbandoned),
		setState("merged", "Mark a change merged, as when it landed somewhere other than its base", store.ChangeMerged),
		setState("reopen", "Reopen an abandoned or merged change", store.ChangeOpen))
	return cmd
}

type changeDetailJSON struct {
	changeJSON
	Revisions []*revisionJSON `json:"revisions"`
	Reviews   []reviewJSON    `json:"reviews"`
	Threads   threadCounts    `json:"threads"`
}

type threadCounts struct {
	Open      int `json:"open"`
	Addressed int `json:"addressed"`
	Resolved  int `json:"resolved"`
	Outdated  int `json:"outdated"`
}

func (a *app) showChange(ctx context.Context, svc *review.Service, change store.Change, rev store.Revision) error {
	revisions, err := svc.Revisions(ctx, change)
	if err != nil {
		return err
	}
	reviews, err := svc.Reviews(ctx, change)
	if err != nil {
		return err
	}
	threads, err := svc.Threads(ctx, change, review.ThreadFilter{})
	if err != nil {
		return err
	}
	var counts threadCounts
	for _, t := range threads {
		switch t.State {
		case store.ThreadOpen:
			counts.Open++
		case store.ThreadAddressed:
			counts.Addressed++
		case store.ThreadResolved:
			counts.Resolved++
		}
		if t.Outdated {
			counts.Outdated++
		}
	}

	if a.json {
		l := newLookups(svc)
		out := changeDetailJSON{changeJSON: toChange(change, &rev), Threads: counts}
		for _, r := range revisions {
			out.Revisions = append(out.Revisions, toRevision(r))
		}
		out.Reviews = []reviewJSON{}
		for _, r := range reviews {
			rj, err := l.review(ctx, r)
			if err != nil {
				return err
			}
			out.Reviews = append(out.Reviews, rj)
		}
		return a.printJSON(out)
	}

	w := a.stdout
	fmt.Fprintf(w, "Change #%d: %s\n", change.Seq, change.Title)
	fmt.Fprintf(w, "%s, %s, revision %d (%s..%s)\n", change.State, describeTarget(change), rev.Seq, short(rev.BaseSHA), short(rev.HeadSHA))
	fmt.Fprintf(w, "Threads: %d open, %d addressed, %d resolved", counts.Open, counts.Addressed, counts.Resolved)
	if counts.Outdated > 0 {
		fmt.Fprintf(w, ", %d outdated", counts.Outdated)
	}
	fmt.Fprintln(w)
	if len(reviews) > 0 {
		fmt.Fprintln(w, "\nReviews:")
		for _, r := range reviews {
			status := string(r.Verdict)
			if r.State == store.ReviewDraft {
				status = "draft"
			}
			fmt.Fprintf(w, "  #%d %s by %s\n", r.Seq, status, r.Author)
			if r.Body != "" {
				fmt.Fprintln(w, indent(r.Body, "    "))
			}
		}
	}
	return nil
}

func describeTarget(c store.Change) string {
	switch c.Kind {
	case store.KindBranch:
		return c.Branch + " onto " + c.BaseBranch
	default:
		return string(c.Kind)
	}
}

func short(sha string) string {
	if sha == "" {
		return "root"
	}
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
