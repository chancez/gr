package tui

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

func (m *model) cursorThread() (review.ThreadView, bool) {
	p := &m.main
	if p.cursor < len(p.rows) && p.rows[p.cursor].thread != nil &&
		(p.rows[p.cursor].kind == rowThread || p.rows[p.cursor].kind == rowReview) {
		return *p.rows[p.cursor].thread, true
	}
	if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowReview {
		m.setError(errors.New("this review has no summary"))
		return review.ThreadView{}, false
	}
	m.setError(errors.New("move the cursor to a thread first"))
	return review.ThreadView{}, false
}

func (m *model) lastOwnComment(t review.ThreadView) (store.Comment, bool) {
	for i := len(t.Comments) - 1; i >= 0; i-- {
		if t.Comments[i].Author == m.svc.Author() {
			return t.Comments[i], true
		}
	}
	return store.Comment{}, false
}

// threadCollapsed reports whether a thread shows as one line.
func (m *model) threadCollapsed(t review.ThreadView) bool {
	if collapsed, ok := m.threadFolds[t.ID]; ok {
		return collapsed
	}
	return t.State == store.ThreadResolved
}

func (m *model) setThreadState(t review.ThreadView, state store.ThreadState) tea.Cmd {
	// Back to the default, so resolving collapses a thread and reopening expands it.
	delete(m.threadFolds, t.ID)
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		return opMsg{status: fmt.Sprintf("thread %s %s", t.Label(), state), err: svc.SetThreadState(ctx, t.ID, state)}
	}
}

func (m *model) threadAction(kind composeKind) (tea.Model, tea.Cmd) {
	t, ok := m.cursorThread()
	if !ok {
		return m, nil
	}
	c := composer{kind: kind, threadID: t.ID}
	initial := ""
	switch kind {
	case composeReply:
		c.title = fmt.Sprintf("Reply to %s", t.Label())
	case composeEdit:
		own, ok := m.lastOwnComment(t)
		// In the overview, edit the selected comment when it is yours.
		if seg := m.main.itemSegment(); m.main.reader && seg < len(t.Comments) && t.Comments[seg].Author == m.svc.Author() {
			own, ok = t.Comments[seg], true
		}
		if !ok {
			m.setError(errors.New("no comment of yours in this thread"))
			return m, nil
		}
		c.title, c.commentID, initial = fmt.Sprintf("Edit your comment on %s", t.Label()), own.ID, own.Body
		if t.State == store.ThreadResolved {
			// Edits after resolving are easy to miss: nobody rereads a resolved thread.
			c.title += " (resolved: the edit will not reopen it)"
			c.warn = true
		}
	}
	return m, m.openComposer(c, initial)
}

// deleteReviewAtCursor deletes the review whose entry the cursor is on, or discards the draft
// when the cursor is elsewhere, after asking.
func (m *model) deleteReviewAtCursor() tea.Cmd {
	svc, ctx, change := m.svc, m.ctx, m.data.change
	p := &m.main
	if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowReview && p.rows[p.cursor].review.State == store.ReviewSubmitted {
		rv := *p.rows[p.cursor].review
		if rv.Author != svc.Author() {
			m.setError(fmt.Errorf("review #%d is by %s", rv.Seq, rv.Author.Name))
			return nil
		}
		m.ask(fmt.Sprintf("Delete your review #%d with its summary and threads? y/n", rv.Seq), func() tea.Msg {
			return opMsg{status: fmt.Sprintf("review #%d deleted", rv.Seq), err: svc.DeleteReview(ctx, change, rv.Seq)}
		})
		return nil
	}
	if _, ok := m.data.draft(); !ok {
		m.setError(errors.New("no draft review; on a review of yours, D deletes it"))
		return nil
	}
	m.ask("Discard your draft review and all its comments? y/n", func() tea.Msg {
		return opMsg{status: "draft review discarded", err: svc.Discard(ctx, change)}
	})
	return nil
}
