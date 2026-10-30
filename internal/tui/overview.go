package tui

import (
	"fmt"
	"strings"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// threadFilter selects which threads the overview shows.
type threadFilter int

const (
	filterAll threadFilter = iota
	filterUnresolved
	filterOpen
	filterAddressed
	filterResolved
	filterCount
)

func (f threadFilter) String() string {
	return [...]string{"all", "unresolved", "open", "addressed", "resolved"}[f]
}

func (f threadFilter) match(t review.ThreadView) bool {
	switch f {
	case filterUnresolved:
		return t.State != store.ThreadResolved
	case filterOpen:
		return t.State == store.ThreadOpen
	case filterAddressed:
		return t.State == store.ThreadAddressed
	case filterResolved:
		return t.State == store.ThreadResolved
	}
	return true
}

// excerpt cuts a thread's excerpt from the loaded diffs. Threads anchored to commits or revisions
// that are not loaded, such as outdated ones, fall back to their stored lines.
func (d *changeData) excerpt(t review.ThreadView) (review.Excerpt, bool) {
	var files []git.FileDiff
	switch loc := t.Location; {
	case t.Outdated:
	case loc.Scope == store.ScopeCommit:
		files = d.commitFiles[loc.Commit]
	case loc.Scope == store.ScopeDiff && loc.RevisionID == d.revision.ID:
		files = d.diffFiles
	}
	return review.ExcerptFor(files, t.Location)
}

// buildOverview lays out the conversation: the change, then each submitted review in order with
// its summary and the threads started in it, then the viewer's pending review. Line threads show
// the diff they are about.
func buildOverview(d *changeData, r *renderer, filter threadFilter) []row {
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
	counts := map[store.ThreadState]int{}
	for _, t := range d.threads {
		counts[t.State]++
	}
	rows := []row{
		plain(
			st.heading.Render(fmt.Sprintf("#%d %s", c.Seq, c.Title)),
			st.muted.Render(fmt.Sprintf("%s, %s, revision %d of %d (%s..%s), %d commits, %d files",
				c.State, target, rev.Seq, len(d.revisions), shortSHA(rev.BaseSHA), shortSHA(rev.HeadSHA),
				len(d.commits), len(d.diffFiles))),
			"Threads: "+st.badgeOpen.Render(fmt.Sprintf("%d open", counts[store.ThreadOpen]))+", "+
				st.badgeWarn.Render(fmt.Sprintf("%d addressed", counts[store.ThreadAddressed]))+", "+
				st.badgeDone.Render(fmt.Sprintf("%d resolved", counts[store.ThreadResolved]))+
				st.muted.Render("   showing "+filter.String()+"; F filters, enter opens a thread in its diff"),
			"",
		),
	}

	byReview := map[string][]int{}
	for i, t := range d.threads {
		byReview[t.ReviewID] = append(byReview[t.ReviewID], i)
	}
	threadRows := func(indexes []int) []row {
		var out []row
		// The summary leads its review.
		for _, summaryFirst := range []bool{true, false} {
			for _, i := range indexes {
				t := &d.threads[i]
				if t.Summary != summaryFirst || !filter.match(*t) {
					continue
				}
				rw := row{kind: rowThread, file: -1, thread: t}
				if e, ok := d.excerpt(*t); ok {
					rw.excerpt = &e
				}
				out = append(out, rw)
			}
		}
		return out
	}
	reviewHeader := func(rv store.Review) row {
		return text(func(width int) []string {
			head := st.author.Render(rv.Author.Name) + " " + verdictLabel(st, rv.Verdict) +
				"  " + st.muted.Render(rv.SubmittedAt.Local().Format("2006-01-02 15:04"))
			return []string{fit(head, width, nil)}
		})
	}

	shown := 0
	for _, rv := range d.reviews {
		if rv.State != store.ReviewSubmitted {
			continue
		}
		threads := threadRows(byReview[rv.ID])
		if len(threads) == 0 && filter != filterAll {
			continue
		}
		rows = append(rows, reviewHeader(rv))
		if !hasSummary(d, rv.ID) && strings.TrimSpace(rv.Body) != "" {
			rows = append(rows, text(func(width int) []string {
				var lines []string
				for _, l := range r.renderMarkdown(rv.Body, width-4) {
					lines = append(lines, "    "+l)
				}
				return lines
			}))
		}
		rows = append(rows, threads...)
		rows = append(rows, plain(""))
		shown += len(threads)
	}
	if draft, ok := d.draft(); ok {
		threads := threadRows(byReview[draft.ID])
		rows = append(rows, plain(st.badgeWarn.Render(fmt.Sprintf(
			"Your pending review has %d new threads. S submits it, D discards it.", len(byReview[draft.ID])))))
		rows = append(rows, threads...)
		shown += len(threads)
	}
	if shown == 0 {
		msg := "No threads yet. c comments on the change, or open a commit or file and comment on lines."
		if filter != filterAll {
			msg = "No " + filter.String() + " threads."
		}
		rows = append(rows, plain(st.muted.Render(msg)))
	}
	return rows
}

func hasSummary(d *changeData, reviewID string) bool {
	for _, t := range d.threads {
		if t.Summary && t.ReviewID == reviewID {
			return true
		}
	}
	return false
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
