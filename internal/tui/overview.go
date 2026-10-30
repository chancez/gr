package tui

import (
	"fmt"
	"strings"

	"github.com/chancez/gr/internal/store"
)

// buildOverview lays out the change summary, reviews, top-level threads, and outdated threads.
func buildOverview(d *changeData, r *renderer) []row {
	st := r.st
	text := func(f func(width int) []string) row { return row{kind: rowText, file: -1, text: f} }
	plain := func(lines ...string) row {
		return text(func(width int) []string {
			out := make([]string, len(lines))
			for i, l := range lines {
				out[i] = fit(l, width, nil)
			}
			return out
		})
	}

	c, rev := d.change, d.revision
	target := string(c.Kind)
	if c.Kind == store.KindBranch {
		target = c.Branch + " onto " + c.BaseBranch
	}
	rows := []row{
		plain(
			st.heading.Render(fmt.Sprintf("#%d %s", c.Seq, c.Title)),
			st.muted.Render(fmt.Sprintf("%s, %s, revision %d of %d (%s..%s), %d commits, %d files",
				c.State, target, rev.Seq, len(d.revisions), shortSHA(rev.BaseSHA), shortSHA(rev.HeadSHA),
				len(d.commits), len(d.diffFiles))),
			"",
		),
	}

	if draft, ok := d.draft(); ok {
		n := 0
		for _, t := range d.threads {
			if t.ReviewID == draft.ID {
				n++
			}
		}
		rows = append(rows, plain(st.badgeWarn.Render(fmt.Sprintf("Your draft review has %d new threads. S submits it, D discards it.", n)), ""))
	}

	var submitted int
	for _, rv := range d.reviews {
		if rv.State == store.ReviewSubmitted {
			submitted++
		}
	}
	if submitted > 0 {
		rows = append(rows, plain(st.heading.Render("Reviews")))
		for _, rv := range d.reviews {
			if rv.State != store.ReviewSubmitted {
				continue
			}
			rows = append(rows, text(func(width int) []string {
				head := fmt.Sprintf("%s %s  %s", st.author.Render(rv.Author.Name), verdictLabel(st, rv.Verdict),
					st.muted.Render(rv.SubmittedAt.Local().Format("2006-01-02 15:04")))
				lines := []string{fit("  "+head, width, nil)}
				if strings.TrimSpace(rv.Body) != "" {
					for _, l := range r.renderMarkdown(rv.Body, width-4) {
						lines = append(lines, "    "+l)
					}
				}
				return append(lines, "")
			}))
		}
	}

	var changeThreads, outdated []int
	for i, t := range d.threads {
		switch {
		case t.Outdated:
			outdated = append(outdated, i)
		case t.Location.Scope == store.ScopeChange:
			changeThreads = append(changeThreads, i)
		}
	}
	rows = append(rows, plain(st.heading.Render("Conversation")+st.muted.Render("  c to comment on the change")))
	for _, i := range changeThreads {
		rows = append(rows, row{kind: rowThread, file: -1, thread: &d.threads[i]})
	}
	if len(outdated) > 0 {
		rows = append(rows, plain("", st.heading.Render("Outdated threads")+st.muted.Render("  their code changed in a later revision")))
		for _, i := range outdated {
			rows = append(rows, row{kind: rowThread, file: -1, thread: &d.threads[i]})
		}
	}
	return rows
}

func verdictLabel(st styles, v store.Verdict) string {
	switch v {
	case store.VerdictApprove:
		return st.badgeOpen.Render("approved")
	case store.VerdictRequestChanges:
		return st.statusErr.Render("requested changes")
	}
	return st.muted.Render("commented")
}
