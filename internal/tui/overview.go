package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/look"
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

// overviewDetails is the change's details and thread counts, pinned at the top of the overview
// under the title bar so they stay put while it scrolls, then a blank line.
func overviewDetails(d *changeData) [][]look.Span {
	c, rev := d.change, d.revision
	target := string(c.Kind)
	if c.Kind == store.KindBranch {
		target = c.Branch + " onto " + c.BaseBranch
	}
	counts := map[store.ThreadState]int{}
	for _, t := range d.threads {
		counts[t.State]++
	}
	return [][]look.Span{
		{look.Heading(fmt.Sprintf("#%d %s", c.Seq, c.Title))},
		append([]look.Span{
			look.Muted(fmt.Sprintf("%s, %s, revision %d of %d (", c.State, target, rev.Seq, len(d.revisions))),
			look.SHA(rev.BaseSHA).Short(), look.Muted(".."), look.SHA(rev.HeadSHA).Short(),
			look.Muted(fmt.Sprintf("), %d commits, %d files, ", len(d.commits), len(d.diffFiles))),
		}, filesSize(d.diffFiles)...),
		{
			look.Default("Threads: "),
			look.StateCount(store.ThreadOpen, counts[store.ThreadOpen]), look.Default(", "),
			look.StateCount(store.ThreadAddressed, counts[store.ThreadAddressed]), look.Default(", "),
			look.StateCount(store.ThreadResolved, counts[store.ThreadResolved]),
			look.Muted("   o opens a thread in the whole diff, O in its commit"),
		},
		{},
	}
}

// buildOverview lays out the conversation: the change, then each submitted review in order with
// its summary and the threads started in it, then the viewer's pending review. Line threads show
// the diff they are about.
func buildOverview(d *changeData, r *renderer, filter threadFilter) []row {
	th := r.th
	text := func(f func(width int) []string) row { return row{kind: rowText, file: -1, text: f} }
	// plain is lines of spans, one row.
	plain := func(lines ...[]look.Span) row {
		return text(func(width int) []string {
			out := make([]string, len(lines))
			for i, l := range lines {
				out[i] = th.Draw(look.Plain, width, l...)
			}
			return out
		})
	}
	blank := []look.Span{}

	rev := d.revision
	var rows []row

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
	// finished reports whether a review has threads and every one is resolved, its summary included.
	finished := func(reviewID string) bool {
		n := 0
		for _, i := range byReview[reviewID] {
			if d.threads[i].State != store.ThreadResolved {
				return false
			}
			n++
		}
		return n > 0
	}
	reviewRow := func(rv *store.Review, summary *review.ThreadView, parts ...[]look.Span) row {
		return row{kind: rowReview, file: -1, review: rv, thread: summary, bar: parts, done: finished(rv.ID)}
	}
	divider := func(label string) row {
		return text(func(width int) []string {
			line := "--- " + label + " "
			return []string{th.Draw(look.Plain, width, look.Muted(line+strings.Repeat("-", max(width-len(line), 0))))}
		})
	}

	// The moves made before each review show above it, and those since the last after it.
	moves := d.events
	eventsBefore := func(at time.Time, all bool) {
		n := 0
		for n < len(moves) && (all || moves[n].CreatedAt.Before(at)) {
			n++
		}
		merged := mergeEvents(moves[:n])
		for _, m := range merged {
			rows = append(rows, divider(m.label()))
		}
		if len(merged) > 0 {
			rows = append(rows, plain(blank))
		}
		moves = moves[n:]
	}

	shown := 0
	lastRev := 0
	for i := range d.reviews {
		rv := &d.reviews[i]
		if rv.State != store.ReviewSubmitted {
			continue
		}
		eventsBefore(rv.SubmittedAt, false)
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
		parts := [][]look.Span{
			{look.Heading(fmt.Sprintf("Review #%d", rv.Seq))},
			{look.Author(rv.Author)},
			{look.Verdict(rv.Verdict)},
			{look.Muted(fmt.Sprintf("revision %d  ", seq)), look.Time(rv.SubmittedAt)},
		}
		if summary != nil {
			parts = append(parts, append([]look.Span{look.Muted("summary"), look.Default(" ")}, look.State(*summary, false)...))
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
	eventsBefore(time.Time{}, true)
	if draft, ok := d.draft(); ok {
		threads := threadRows(byReview[draft.ID])
		if lastRev != 0 && lastRev != rev.Seq {
			rows = append(rows, divider(fmt.Sprintf("branch updated: revision %d -> %d", lastRev, rev.Seq)), plain(blank))
		}
		rows = append(rows, reviewRow(&draft, nil,
			[]look.Span{look.Heading("Your pending review")},
			[]look.Span{look.Draft(fmt.Sprintf("%d new threads", len(byReview[draft.ID]))).With(look.Strong)},
			[]look.Span{look.Muted("S submits it, D discards it")},
		))
		rows = append(rows, threads...)
		shown += len(threads) + 1
	}
	if shown == 0 {
		msg := "No threads yet. c comments on the change, or open a commit or file and comment on lines."
		if filter != filterAll {
			msg = "No " + filter.String() + " threads."
		}
		rows = append(rows, plain([]look.Span{look.Muted(msg)}))
	}
	return rows
}

// mergedEvent is a run of moves of one kind with no review between them, shown as one: from where
// the first started to where the last ended, so trying a few bases before settling on one is one
// line.
type mergedEvent struct {
	kind  store.EventKind
	how   store.EventHow
	from  string
	to    string
	count int
}

// mergeEvents merges moves, which no review comes between, by kind, in the order each kind last
// moved. A run that ends where it started moved nothing, and is left out.
func mergeEvents(moves []store.Event) []mergedEvent {
	var out []mergedEvent
	for _, mv := range moves {
		i := slices.IndexFunc(out, func(m mergedEvent) bool { return m.kind == mv.Kind })
		if i < 0 {
			out = append(out, mergedEvent{kind: mv.Kind, how: mv.How, from: mv.From, to: mv.To, count: 1})
			continue
		}
		m := out[i]
		m.to, m.count = mv.To, m.count+1
		if mv.How != m.how {
			m.how = store.ByHand
		}
		// It moves to the end, as the kind moved last.
		out = append(slices.Delete(out, i, i+1), m)
	}
	return slices.DeleteFunc(out, func(m mergedEvent) bool { return m.from == m.to })
}

// label is how the overview's timeline says what moved.
func (m mergedEvent) label() string {
	what := "base changed"
	if m.kind == store.EventBranch {
		what = "branch moved"
		if m.how == store.ByRename {
			what = "branch renamed"
		}
	}
	label := fmt.Sprintf("%s: %s -> %s", what, m.from, m.to)
	if m.count > 1 {
		label += fmt.Sprintf(" (%d moves)", m.count)
	}
	return label
}

func hasSummary(d *changeData, reviewID string) bool {
	for _, t := range d.threads {
		if t.Summary && t.ReviewID == reviewID {
			return true
		}
	}
	return false
}
