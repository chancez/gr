package tui

import (
	"errors"
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

func (m *model) rebuildMain() {
	if m.data == nil {
		return
	}
	v := m.views[m.current]
	m.main.reader = v.kind == viewOverview
	m.main.marker = m.r.hunkMarker(rowState{activeHunk: true})
	m.segments = map[int][]int{}
	m.main.segments = func(i int) []int { return m.segments[i] }
	if v.kind == viewOverview {
		m.main.rows = buildOverview(m.data, m.r, m.filter)
	} else {
		m.main.rows = buildRows(m.data, v, rowOptions{
			split:     m.split,
			collapsed: func(f int) bool { return m.collapsed(v, f) },
			reviewed:  func(f int) bool { return m.fileReviewed(v, f) },
			gapOpen:   func(f, g int) bool { return m.openGaps[gapKey(v, v.files[f].Path(), g)] },
			content:   func(f int) []string { return m.fileContent(v, f) },
		})
	}
	for i := range m.main.rows {
		if t := m.main.rows[i].thread; t != nil && (m.main.rows[i].kind == rowThread || m.main.rows[i].kind == rowReview) {
			m.main.rows[i].collapsed = m.threadCollapsed(*t)
		}
	}
	m.main.anchor = -1
	_, mw, bh := m.layoutSizes()
	m.main.width, m.main.height = mw, mainHeight(bh)
	m.main.layout(m.renderMainRow)
}

func (m *model) renderMainRow(i int, state rowState) []string {
	state.dim = m.focus != focusMain
	width := m.main.width
	if m.main.reader {
		// One column is the margin that marks the selected item.
		width--
	}
	lines := m.r.renderRow(m.data, m.views[m.current], m.main.rows[i], width, state)
	if m.r.segments != nil {
		m.segments[i] = m.r.segments
	}
	return lines
}

func (m *model) setView(i int) {
	if i == m.current || i < 0 || i >= len(m.views) {
		return
	}
	if m.data != nil && !m.restoring && m.current < len(m.views) {
		m.pushJump(m.here())
	}
	m.current = i
	m.main.cursor, m.main.offset = 0, 0
	m.rebuildMain()
	m.buildNav()
	m.requestLoad()
}

func (m *model) mainKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	p := &m.main
	if p.reader {
		switch {
		case key.Matches(msg, k.Up):
			p.readerPrev()
			return m, nil
		case key.Matches(msg, k.Down):
			p.readerNext()
			return m, nil
		case key.Matches(msg, k.HalfPageDown):
			p.readerScroll(p.height / 2)
			return m, nil
		case key.Matches(msg, k.HalfPageUp):
			p.readerScroll(-p.height / 2)
			return m, nil
		case key.Matches(msg, k.PageDown):
			p.readerScroll(p.height)
			return m, nil
		case key.Matches(msg, k.PageUp):
			p.readerScroll(-p.height)
			return m, nil
		case key.Matches(msg, k.Top):
			m.jump(func() { p.readerSelect(0) })
			return m, nil
		case key.Matches(msg, k.Bottom):
			m.jump(func() { p.readerSelect(len(p.items) - 1) })
			return m, nil
		}
	}
	switch {
	case key.Matches(msg, k.Up):
		p.move(-1)
	case key.Matches(msg, k.Down):
		p.move(1)
	case key.Matches(msg, k.HalfPageDown):
		p.moveLines(p.height / 2)
	case key.Matches(msg, k.HalfPageUp):
		p.moveLines(-p.height / 2)
	case key.Matches(msg, k.PageDown):
		p.moveLines(p.height)
	case key.Matches(msg, k.PageUp):
		p.moveLines(-p.height)
	case key.Matches(msg, k.Top):
		m.jump(p.top)
	case key.Matches(msg, k.Bottom):
		m.jump(p.bottom)
	case key.Matches(msg, k.Left):
		// In split mode, left first moves to the old side, then to the sidebar.
		if m.split && p.side == anchor.SideNew {
			p.side = anchor.SideOld
		} else {
			m.focusSidebar()
		}
	case key.Matches(msg, k.Right):
		p.side = anchor.SideNew
	case key.Matches(msg, k.NextFile):
		m.jumpTo(1, func(r row) bool { return r.kind == rowFile })
	case key.Matches(msg, k.PrevFile):
		m.jumpTo(-1, func(r row) bool { return r.kind == rowFile })
	case key.Matches(msg, k.NextChange):
		m.jumpToIndex(1, m.isChangeStart)
	case key.Matches(msg, k.PrevChange):
		m.jumpToIndex(-1, m.isChangeStart)
	case key.Matches(msg, k.NextHunk):
		m.jumpTo(1, func(r row) bool { return r.kind == rowHunk })
	case key.Matches(msg, k.PrevHunk):
		m.jumpTo(-1, func(r row) bool { return r.kind == rowHunk })
	case key.Matches(msg, k.NextThread):
		m.jump(func() { m.jumpTo(1, func(r row) bool { return r.kind == rowThread }) })
	case key.Matches(msg, k.PrevThread):
		m.jump(func() { m.jumpTo(-1, func(r row) bool { return r.kind == rowThread }) })
	case key.Matches(msg, k.Select):
		if p.anchor >= 0 {
			p.anchor = -1
		} else if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowLine {
			p.anchor = p.cursor
		}
	case key.Matches(msg, k.OpenThread), key.Matches(msg, k.OpenThreadCommit):
		if m.views[m.current].kind == viewOverview && p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowThread {
			m.openThread(*p.rows[p.cursor].thread, key.Matches(msg, k.OpenThread))
		}
	case key.Matches(msg, k.Filter):
		if m.views[m.current].kind == viewOverview {
			m.filter = (m.filter + 1) % filterCount
			m.rebuildMain()
			m.setStatus("showing %s threads", m.filter)
		}
	case key.Matches(msg, k.Open):
		if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowFile {
			m.toggleCollapsed(m.views[m.current], p.rows[p.cursor].file)
		} else if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowGap {
			m.fold(foldOpen)
		} else if p.cursor < len(p.rows) && (p.rows[p.cursor].kind == rowThread || p.rows[p.cursor].kind == rowReview) {
			m.fold(foldToggle)
		}
	case key.Matches(msg, k.Comment):
		return m.startComment(false)
	case key.Matches(msg, k.FileComment):
		return m.startComment(true)
	case key.Matches(msg, k.Reply):
		return m.threadAction(composeReply)
	case key.Matches(msg, k.Edit):
		return m.threadAction(composeEdit)
	case key.Matches(msg, k.Resolve):
		t, ok := m.cursorThread()
		if !ok {
			return m, nil
		}
		state := store.ThreadResolved
		if t.State == store.ThreadResolved {
			state = store.ThreadOpen
		}
		return m, m.setThreadState(t, state)
	case key.Matches(msg, k.Address):
		t, ok := m.cursorThread()
		if !ok {
			return m, nil
		}
		return m, m.setThreadState(t, store.ThreadAddressed)
	case key.Matches(msg, k.Delete):
		t, ok := m.cursorThread()
		if !ok {
			return m, nil
		}
		c, ok := m.lastOwnComment(t)
		// In the overview, delete the selected comment when it is yours.
		if seg := m.main.itemSegment(); m.main.reader && seg < len(t.Comments) && t.Comments[seg].Author == m.svc.Author() {
			c, ok = t.Comments[seg], true
		}
		if !ok {
			m.setError(errors.New("no comment of yours in this thread"))
			return m, nil
		}
		svc, ctx := m.svc, m.ctx
		prompt := fmt.Sprintf("Delete your comment on %s? y/n", t.Label())
		status := "comment deleted"
		if len(t.Comments) > 0 && t.Comments[0].ID == c.ID {
			prompt = fmt.Sprintf("Delete thread %s? Its first comment is the thread. y/n", t.Label())
			status = "thread " + t.Label() + " deleted"
		}
		m.ask(prompt, func() tea.Msg {
			return opMsg{status: status, err: svc.DeleteComment(ctx, c.ID)}
		})
		return m, nil
	default:
		return m, nil
	}
	m.syncNav()
	return m, nil
}

// toggleAllChanges opens the whole diff, or goes back to the view it was opened from.
func (m *model) toggleAllChanges() { m.toggleView(viewDiff) }

// toggleView opens the view of a kind, or, from that view, goes back exactly where it was opened
// from: the same view, row, and screen line.
func (m *model) toggleView(kind viewKind) {
	if m.views[m.current].kind == kind {
		if back, ok := m.returns[kind]; ok {
			delete(m.returns, kind)
			m.pushJump(m.here())
			m.restore(back)
		}
		return
	}
	for i, v := range m.views {
		if v.kind == kind {
			m.returns[kind] = m.here()
			m.viewsNav.pos = i
			m.openView()
			return
		}
	}
}

// openThread shows a thread in the commit or diff it is anchored to, with the cursor on it.
//
// With inDiff, it prefers the whole diff, where a thread on a commit shows at the place its code
// is now, so a fix made after the comment shows beside it; it falls back to the thread's commit.
func (m *model) openThread(t review.ThreadView, inDiff bool) {
	if t.Outdated {
		m.setError(errors.New("the code this thread is on changed; it is shown as it was"))
		return
	}
	if inDiff && t.Location.Scope == store.ScopeCommit && t.Location.StartLine > 0 && !m.data.loaded[view{kind: viewDiff}.key()] && !m.syncLoads {
		// Where the thread shows in the whole diff is known once the diff loads.
		for i, v := range m.views {
			if v.kind == viewDiff {
				m.pendingThread = &t
				m.viewsNav.pos = i
				m.openView()
				return
			}
		}
	}
	if inDiff && t.Location.Scope == store.ScopeCommit && t.Location.StartLine > 0 && m.syncLoads {
		for _, v := range m.views {
			if v.kind == viewDiff && m.needsLoad(v) {
				m.data.apply(loadView(m.ctx, m.svc, m.data, v))
				m.views = buildViews(m.data)
			}
		}
	}
	target := -1
	for i, v := range m.views {
		if _, ok := m.data.locationIn(&t, v); ok && (v.kind == viewCommit || v.kind == viewDiff) {
			if target < 0 || (inDiff && v.kind == viewDiff) || (!inDiff && v.kind == viewCommit) {
				target = i
			}
		}
	}
	if target < 0 {
		m.setError(errors.New("this thread is on the whole change"))
		return
	}
	m.viewsNav.pos = target
	m.openView()
	v := m.views[m.current]
	for i, r := range m.main.rows {
		if r.kind == rowThread && r.thread.ID == t.ID {
			m.main.jump(i)
			m.syncNav()
			return
		}
	}
	// Collapsed files hide their threads: open the file and look again.
	for f, file := range v.files {
		if file.Path() == t.Location.Path && m.collapsed(v, f) {
			m.toggleCollapsed(v, f)
			m.openThread(t, inDiff)
			return
		}
	}
}

// stepCommit opens the next or previous commit, starting from the first or last when the current
// view is not a commit.
func (m *model) stepCommit(dir int) bool {
	return m.stepCommitWhere(dir, func(view) bool { return true })
}

// stepUnreviewed opens the next or previous commit not yet marked reviewed.
func (m *model) stepUnreviewed(dir int) {
	if !m.stepCommitWhere(dir, func(v view) bool { return !m.viewReviewed(v) }) {
		way := "after"
		if dir < 0 {
			way = "before"
		}
		m.setStatus("no unreviewed commit %s this one", way)
	}
}

// stepCommitWhere opens the next or previous commit that match accepts.
func (m *model) stepCommitWhere(dir int, match func(view) bool) bool {
	for i := m.current + dir; i >= 0 && i < len(m.views); i += dir {
		if m.views[i].kind == viewCommit && match(m.views[i]) {
			m.viewsNav.pos = i
			m.openView()
			return true
		}
	}
	return false
}

func (m *model) jumpTo(dir int, match func(row) bool) {
	m.jumpToIndex(dir, func(i int) bool { return match(m.main.rows[i]) })
}

func (m *model) jumpToIndex(dir int, match func(i int) bool) {
	p := &m.main
	for i := p.cursor + dir; i >= 0 && i < len(p.rows); i += dir {
		if match(i) {
			p.jump(i)
			return
		}
	}
}

// isChange reports whether a row shows an added or deleted line.
func isChange(r row) bool {
	if r.kind != rowLine {
		return false
	}
	changed := func(l *git.Line) bool { return l != nil && l.Kind != git.LineContext }
	if r.line != nil {
		return changed(r.line)
	}
	return changed(r.left) || changed(r.right)
}

// isChangeStart reports whether row i starts a run of added and deleted lines. Threads between
// changed lines do not split a run.
func (m *model) isChangeStart(i int) bool {
	rows := m.main.rows
	if !isChange(rows[i]) {
		return false
	}
	for j := i - 1; j >= 0; j-- {
		if rows[j].kind == rowThread {
			continue
		}
		return !isChange(rows[j]) || rows[j].hunk != rows[i].hunk
	}
	return true
}
