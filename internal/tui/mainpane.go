package tui

import (
	"errors"

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
				m.pendingThread, m.pendingInDiff = &t, true
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
	if m.needsLoad(v) && !m.syncLoads {
		// Its rows are built once its diff arrives.
		m.pendingThread, m.pendingInDiff = &t, inDiff
		return
	}
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

// stepUnresolved moves to the next or previous unresolved thread: further on in the view, or else
// the nearest in the next or previous commit that has one, as a jump ctrl+o returns from.
func (m *model) stepUnresolved(dir int) {
	way := "after"
	if dir < 0 {
		way = "before"
	}
	before := m.main.cursor
	m.jump(func() { m.jumpTo(dir, func(r row) bool { return r.kind == rowThread && unresolved(r.thread) }) })
	if m.main.cursor != before {
		m.syncNav()
		return
	}
	if m.views[m.current].kind == viewCommit {
		for i := m.current + dir; i >= 0 && i < len(m.views); i += dir {
			if t, ok := m.nearestUnresolved(m.views[i], dir); ok && m.views[i].kind == viewCommit {
				m.jump(func() { m.openThread(t, false) })
				return
			}
		}
	}
	m.setStatus("no unresolved thread %s this one", way)
}

func unresolved(t *review.ThreadView) bool {
	return t != nil && !t.Summary && t.State != store.ThreadResolved
}

// nearestUnresolved is v's first unresolved thread, in file and line order, or its last when dir
// is back.
func (m *model) nearestUnresolved(v view, dir int) (review.ThreadView, bool) {
	file := map[string]int{}
	for i, f := range v.files {
		file[f.Path()] = i
	}
	var best *review.ThreadView
	var bestAt [2]int
	for i := range m.data.threads {
		t := &m.data.threads[i]
		loc, ok := m.data.locationIn(t, v)
		if !ok || !unresolved(t) {
			continue
		}
		f, inFile := file[loc.Path]
		if !inFile {
			f = -1
		}
		at := [2]int{f, loc.StartLine}
		later := at[0] > bestAt[0] || at[0] == bestAt[0] && at[1] > bestAt[1]
		if best == nil || later == (dir < 0) {
			best, bestAt = t, at
		}
	}
	if best == nil {
		return review.ThreadView{}, false
	}
	return *best, true
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

// Moves: the cursor through the pane focus is in. In the sidebar, landing on an entry runs its on
// cursor handler, so the main pane follows.

// focusedList is the sidebar list focus is in, or nil in the main pane.
func (m *model) focusedList() *navList {
	switch m.focus {
	case focusViews:
		return &m.viewsNav
	case focusFiles:
		return &m.filesNav
	}
	return nil
}

// landed runs the on cursor handler of the sidebar entry the cursor moved to.
func (m *model) landed(moved bool) tea.Cmd {
	if !moved {
		return nil
	}
	if t, ok := m.cursorTarget(); ok {
		return t.OnCursor(m)
	}
	return nil
}

// moves are the moves through one pane, each a pane's own: the sidebar's lists, the diff, or the
// overview's reader.
type moves struct {
	by   func(m *model, n int) tea.Cmd
	page func(m *model, dir, div int) tea.Cmd
	end  func(m *model, bottom bool) tea.Cmd
}

// listMoves move the selection of the sidebar list focus is in, and run the on cursor handler of
// where it lands.
var listMoves = moves{
	by: func(m *model, n int) tea.Cmd { return m.landed(m.focusedList().move(n)) },
	page: func(m *model, dir, div int) tea.Cmd {
		l := m.focusedList()
		_, _, bh := m.layoutSizes()
		vh, fh := m.sidebarHeights(bh)
		page := vh - 1
		if l == &m.filesNav {
			page = fh - 1
		}
		return m.landed(l.move(dir * max(page/div, 1)))
	},
	end: func(m *model, bottom bool) tea.Cmd {
		l := m.focusedList()
		i := 0
		if bottom {
			i = len(l.items) - 1
		}
		return m.landed(l.set(i))
	},
}

// diffMoves move the cursor through the diff by rows.
var diffMoves = moves{
	by: func(m *model, n int) tea.Cmd {
		m.main.move(n)
		m.syncNav()
		return nil
	},
	page: func(m *model, dir, div int) tea.Cmd {
		m.main.moveLines(dir * m.main.height / div)
		m.syncNav()
		return nil
	},
	end: func(m *model, bottom bool) tea.Cmd {
		if bottom {
			m.jump(m.main.bottom)
		} else {
			m.jump(m.main.top)
		}
		m.syncNav()
		return nil
	},
}

// readerMoves move the overview's reader by comment.
var readerMoves = moves{
	by: func(m *model, n int) tea.Cmd {
		if n < 0 {
			m.main.readerPrev()
		} else {
			m.main.readerNext()
		}
		return nil
	},
	page: func(m *model, dir, div int) tea.Cmd {
		m.main.readerScroll(dir * m.main.height / div)
		return nil
	},
	end: func(m *model, bottom bool) tea.Cmd {
		p := &m.main
		if bottom {
			m.jump(func() { p.readerSelect(len(p.items) - 1) })
		} else {
			m.jump(func() { p.readerSelect(0) })
		}
		m.syncNav()
		return nil
	},
}

// moveLeft in split mode first moves to the old side, then to the sidebar.
func (m *model) moveLeft() {
	if m.split && m.main.side == anchor.SideNew {
		m.main.side = anchor.SideOld
		return
	}
	m.focusSidebar()
}

// toggleSelection starts a selection of lines at the cursor, or ends one.
func (m *model) toggleSelection() {
	p := &m.main
	if p.anchor >= 0 {
		p.anchor = -1
	} else if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowLine {
		p.anchor = p.cursor
	}
}
