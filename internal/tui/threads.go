package tui

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

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

// threadActionOn opens the comment box to reply to t, or edit your comment in it: in the
// overview, the selected comment when t is at the cursor and the comment is yours.
func (m *model) threadActionOn(t review.ThreadView, atCursor bool, kind composeKind) tea.Cmd {
	c := composer{kind: kind, threadID: t.ID}
	initial := ""
	switch kind {
	case composeReply:
		c.title = fmt.Sprintf("Reply to %s", t.Label())
	case composeEdit:
		own, ok := m.lastOwnComment(t)
		// In the overview, edit the selected comment when it is yours.
		if seg := m.main.itemSegment(); atCursor && m.main.reader && seg < len(t.Comments) && t.Comments[seg].Author == m.svc.Author() {
			own, ok = t.Comments[seg], true
		}
		if !ok {
			m.setError(errors.New("no comment of yours in this thread"))
			return nil
		}
		c.title, c.commentID, initial = fmt.Sprintf("Edit your comment on %s", t.Label()), own.ID, own.Body
		if t.State == store.ThreadResolved {
			// Edits after resolving are easy to miss: nobody rereads a resolved thread.
			c.title += " (resolved: the edit will not reopen it)"
			c.warn = true
		}
	}
	return m.openComposer(c, initial)
}

// deleteOwnComment asks to delete your last comment in t, or in the overview the selected one when
// it is yours. Deleting a thread's first comment deletes the thread.
func (m *model) deleteOwnComment(t review.ThreadView, atCursor bool) {
	c, ok := m.lastOwnComment(t)
	if seg := m.main.itemSegment(); atCursor && m.main.reader && seg < len(t.Comments) && t.Comments[seg].Author == m.svc.Author() {
		c, ok = t.Comments[seg], true
	}
	if !ok {
		m.setError(errors.New("no comment of yours in this thread"))
		return
	}
	svc, ctx := m.svc, m.ctx
	prompt := fmt.Sprintf("Delete your comment on %s?", t.Label())
	status := "comment deleted"
	if len(t.Comments) > 0 && t.Comments[0].ID == c.ID {
		prompt = fmt.Sprintf("Delete thread %s? Its first comment is the thread.", t.Label())
		status = "thread " + t.Label() + " deleted"
	}
	m.ask(prompt, func() tea.Msg {
		return opMsg{status: status, err: svc.DeleteComment(ctx, c.ID)}
	})
}

// deleteReview asks to delete rv, a review of yours that was submitted.
func (m *model) deleteReview(rv store.Review) tea.Cmd {
	svc, ctx, change := m.svc, m.ctx, m.data.change
	if rv.Author != svc.Author() {
		m.setError(fmt.Errorf("review #%d is by %s", rv.Seq, rv.Author.Name))
		return nil
	}
	m.ask(fmt.Sprintf("Delete your review #%d with its summary and threads?", rv.Seq), func() tea.Msg {
		return opMsg{status: fmt.Sprintf("review #%d deleted", rv.Seq), err: svc.DeleteReview(ctx, change, rv.Seq)}
	})
	return nil
}
