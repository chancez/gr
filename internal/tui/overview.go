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
	if !t.Outdated {
		files = d.threadFiles[t.ID]
	}
	return review.ExcerptFor(files, t.Location)
}

// buildOverview lays out the conversation: the change, then each submitted review in order with
// its summary and the threads started in it, then the viewer's pending review. Line threads show
// the diff they are about.
func buildOverview(d *changeData, r *renderer, filter threadFilter) []row {
	th := r.th
	text := func(f func(width int) []string) row { return row{kind: rowText, file: -1, text: f} }
	// plain is lines of spans, one row.
	plain := func(lines ...[]span) row {
		return text(func(width int) []string {
			out := make([]string, len(lines))
			for i, l := range lines {
				out[i] = th.draw(surfPlain, width, l...)
			}
			return out
		})
	}
	blank := []span{}

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
			[]span{sp(elHeading, fmt.Sprintf("#%d %s", c.Seq, c.Title))},
			[]span{
				sp(elMuted, fmt.Sprintf("%s, %s, revision %d of %d (", c.State, target, rev.Seq, len(d.revisions))),
				sp(elSHA, shortSHA(rev.BaseSHA)), sp(elMuted, ".."), sp(elSHA, shortSHA(rev.HeadSHA)),
				sp(elMuted, fmt.Sprintf("), %d commits, %d files", len(d.commits), len(d.diffFiles))),
			},
			[]span{
				sp(elDefault, "Threads: "),
				sp(elStateOpen, fmt.Sprintf("%d open", counts[store.ThreadOpen])), sp(elDefault, ", "),
				sp(elStateAddressed, fmt.Sprintf("%d addressed", counts[store.ThreadAddressed])), sp(elDefault, ", "),
				sp(elStateResolved, fmt.Sprintf("%d resolved", counts[store.ThreadResolved])),
				sp(elMuted, "   showing "+filter.String()+"; F filters, o opens a thread in the whole diff, O in its commit"),
			},
			blank,
		),
	}

	byReview := map[string][]int{}
	for i, t := range d.threads {
		byReview[t.ReviewID] = append(byReview[t.ReviewID], i)
	}
	threadRows := func(indexes []int) []row {
		var out []row
		for _, i := range indexes {
			t := &d.threads[i]
			// Summaries are drawn with their review, not as threads.
			if t.Summary || !filter.match(*t) {
				continue
			}
			rw := row{kind: rowThread, file: -1, thread: t}
			if e, ok := d.excerpt(*t); ok {
				rw.excerpt = &e
			}
			out = append(out, rw)
		}
		return out
	}
	summaryOf := func(reviewID string) *review.ThreadView {
		for i := range d.threads {
			if d.threads[i].Summary && d.threads[i].ReviewID == reviewID {
				return &d.threads[i]
			}
		}
		return nil
	}
	revSeq := map[string]int{}
	for _, rv := range d.revisions {
		revSeq[rv.ID] = rv.Seq
	}
	// reviewRow is a review's entry: a full-width header bar of separately colored parts, so the
	// verdict stands out, with the summary and its replies drawn beneath it. It is selectable, so
	// the review can be acted on even when it has no threads.
	reviewRow := func(rv *store.Review, summary *review.ThreadView, parts ...[]span) row {
		bar := func(width int) []string {
			spans := []span{sp(elDefault, " ")}
			for i, p := range parts {
				if i > 0 {
					spans = append(spans, sp(elDefault, "  "))
				}
				spans = append(spans, p...)
			}
			return []string{th.draw(surfPlain, width, spans...)}
		}
		return row{kind: rowReview, file: -1, text: bar, review: rv, thread: summary}
	}
	divider := func(label string) row {
		return text(func(width int) []string {
			line := "--- " + label + " "
			return []string{th.draw(surfPlain, width, sp(elMuted, line+strings.Repeat("-", max(width-len(line), 0))))}
		})
	}

	shown := 0
	lastRev := 0
	for i := range d.reviews {
		rv := &d.reviews[i]
		if rv.State != store.ReviewSubmitted {
			continue
		}
		threads := threadRows(byReview[rv.ID])
		summary := summaryOf(rv.ID)
		summaryShown := summary != nil && filter.match(*summary)
		if len(threads) == 0 && !summaryShown && filter != filterAll {
			continue
		}
		seq := revSeq[rv.RevisionID]
		if lastRev != 0 && seq != lastRev {
			rows = append(rows, divider(fmt.Sprintf("branch updated: revision %d -> %d", lastRev, seq)), plain(blank))
		}
		lastRev = seq
		parts := [][]span{
			{sp(elHeading, fmt.Sprintf("Review #%d", rv.Seq))},
			{sp(elAuthor, rv.Author.Name)},
			{sp(verdictElement(rv.Verdict), verdictText(rv.Verdict))},
			{sp(elMuted, fmt.Sprintf("revision %d  %s", seq, rv.SubmittedAt.Local().Format("2006-01-02 15:04")))},
		}
		if summary != nil {
			parts = append(parts, []span{sp(elMuted, "summary"), sp(elDefault, " "+spanText(stateSpans(*summary, false)))})
		}
		// A summary the filter leaves out shows only in the bar's state.
		shownSummary := summary
		if !summaryShown {
			shownSummary = nil
		}
		rows = append(rows, reviewRow(rv, shownSummary, parts...))
		if summary == nil && strings.TrimSpace(rv.Body) != "" {
			rows = append(rows, text(func(width int) []string {
				var lines []string
				for _, l := range r.renderMarkdown(rv.Body, width-4) {
					lines = append(lines, "    "+l)
				}
				return lines
			}))
		}
		rows = append(rows, threads...)
		rows = append(rows, plain(blank))
		shown += len(threads) + 1
	}
	if draft, ok := d.draft(); ok {
		threads := threadRows(byReview[draft.ID])
		if lastRev != 0 && lastRev != rev.Seq {
			rows = append(rows, divider(fmt.Sprintf("branch updated: revision %d -> %d", lastRev, rev.Seq)), plain(blank))
		}
		rows = append(rows, reviewRow(&draft, nil,
			[]span{sp(elHeading, "Your pending review")},
			[]span{sp(elDraft, fmt.Sprintf("%d new threads", len(byReview[draft.ID])), strong)},
			[]span{sp(elMuted, "S submits it, D discards it")},
		))
		rows = append(rows, threads...)
		shown += len(threads) + 1
	}
	if shown == 0 {
		msg := "No threads yet. c comments on the change, or open a commit or file and comment on lines."
		if filter != filterAll {
			msg = "No " + filter.String() + " threads."
		}
		rows = append(rows, plain([]span{sp(elMuted, msg)}))
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

func verdictText(v store.Verdict) string {
	switch v {
	case store.VerdictApprove:
		return "approved"
	case store.VerdictRequestChanges:
		return "requested changes"
	}
	return "commented"
}

// verdictElement is how a verdict is drawn: approved and requested changes in their own colors,
// commented plainly.
func verdictElement(v store.Verdict) element {
	switch v {
	case store.VerdictApprove:
		return elVerdictApprove
	case store.VerdictRequestChanges:
		return elVerdictRequestChanges
	}
	return elVerdictComment
}
