package tui

import (
	"fmt"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/interact"
	"github.com/chancez/gr/internal/tui/look"
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
	m.main.width, m.main.height = mw, m.rowsHeight(bh)
	// The rows' controls go with the rows' drawings.
	m.drawnControls = nil
	if m.mode == modeCompose {
		// The comment box stays under the row it opened under, wherever that row now is.
		m.main.attach = m.main.cursor
		for i, r := range m.main.rows {
			if m.compose.at != "" && r.key(v.files) == m.compose.at {
				m.main.attach = i
			}
		}
	}
	m.main.layout(m.renderMainRow)
}

// stickyFile is the header row of the file the top of the diff is in, once that header has
// scrolled out of view, so it can stay pinned at the top, as GitHub's file headers do.
func (m *model) stickyFile() (int, bool) {
	p := &m.main
	if p.reader {
		return 0, false
	}
	first := p.rowAt(0)
	if first < 0 || p.rows[first].file < 0 || isMessage(m.views[m.current].files[p.rows[first].file]) {
		return 0, false
	}
	for h := first; h >= 0; h-- {
		if r := p.rows[h]; r.kind == rowFile && r.file == p.rows[first].file {
			return h, p.starts[h] < p.offset
		}
	}
	return 0, false
}

func (m *model) renderMainRow(i int, state rowState) []string {
	state.dim = m.focus != focusMain
	state.split = m.split
	width := m.main.width
	if m.main.reader {
		// One column is the margin that marks the selected item.
		width--
	}
	m.rowControls(i, &state)
	lines := m.r.renderRow(m.data, m.views[m.current], m.main.rows[i], width, state)
	if m.r.segments != nil {
		m.segments[i] = m.r.segments
	}
	return lines
}

// focusedThread is the id of the thread the mouse is over, or else the one the cursor is on while
// the diff has focus, whose lines show as its own. It is empty for none.
func (m *model) focusedThread() string {
	p := &m.main
	if m.mode != modeNormal {
		return ""
	}
	if p.hovering && p.hover < len(p.rows) && p.rows[p.hover].kind == rowThread {
		return p.rows[p.hover].thread.ID
	}
	if m.focus == focusMain && p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowThread {
		return p.rows[p.cursor].thread.ID
	}
	return ""
}

// rowControls sets the controls row i shows, in state: a file's chevron before it, and the rest
// after what they act on. Inside the diff they are quiet, and lit where the cursor or the mouse is,
// while the main pane has focus and no mode is open, in which they also do nothing. The + that
// comments on a line shows only under the mouse.
func (m *model) rowControls(i int, state *rowState) {
	r := m.main.rows[i]
	normal := m.mode == modeNormal
	hovered := state.hover && normal
	lit := state.cursor && m.focus == focusMain && normal || hovered
	where := "row" + strconv.Itoa(i)
	controls := func(t interact.Target[*model], cs ...control) []look.Span {
		for k := range cs {
			cs[k] = cs[k].lighted(lit)
		}
		return m.controls(where, modeNormal, t, cs...)
	}
	switch {
	case r.kind == rowLine && !hovered:
		return
	}
	side := m.main.side
	if hovered && m.split {
		side = m.main.hoverSide
	}
	t, ok := m.mainRowTarget(i, side)
	if !ok {
		return
	}
	switch r.kind {
	case rowLine:
		state.controls = controls(t, commentHere)
	case rowGap:
		state.controls = controls(t, expandFold.labeled(gapLabel(*r.gap)))
	case rowCommit:
		state.controls = controls(t, commitViewedBox.checked(m.viewReviewed(m.views[m.current])))
	case rowThread:
		// A thread folds and unfolds from its chevron; its buttons show only while it is open.
		state.lead = controls(t, threadFold.checked(!r.collapsed).heading(append([]look.Span{look.Default(" ")}, threadName(*r.thread)...)...))
		if r.collapsed {
			return
		}
		resolve := resolveButton
		if r.thread.State == store.ThreadResolved {
			resolve = reopenButton
		}
		cs := []control{replyButton, resolve}
		if r.thread.State == store.ThreadOpen {
			cs = append(cs, addressedButton)
		}
		state.controls = controls(t, append(cs, editButton, deleteButton)...)
	case rowReview:
		state.controls = controls(t, deleteReview)
	case rowFile:
		state.lead = controls(t, fileFold.checked(!r.collapsed).heading(look.File(m.views[m.current].files[r.file])))
		state.controls = controls(t, viewedBox.checked(r.reviewed))
		if len(state.controls) == 0 && r.reviewed {
			// Where it cannot be marked, as in the interdiff, the file still says it is reviewed.
			state.controls = []look.Span{look.Reviewed()}
		}
	}
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
			m.viewsNav.show(i, -1)
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
		m.openWhereItWas(t, inDiff)
		return
	}
	if inDiff && t.Location.Scope == store.ScopeCommit && t.Location.StartLine > 0 && !m.data.loaded[view{kind: viewDiff}.key()] && !m.syncLoads {
		// Where the thread shows in the whole diff is known once the diff loads.
		for i, v := range m.views {
			if v.kind == viewDiff {
				m.pendingThread, m.pendingInDiff = &t, true
				m.viewsNav.show(i, -1)
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
		if t.Location.Scope == store.ScopeChange {
			m.setError(fmt.Errorf("thread %s is on the whole change", t.Label()))
		} else {
			m.setError(fmt.Errorf("thread %s is in no commit or diff of this revision", t.Label()))
		}
		return
	}
	m.viewsNav.show(target, -1)
	m.openView()
	v := m.views[m.current]
	if inDiff && v.kind == viewCommit {
		m.setStatus("thread %s is shown in its commit: a later commit changes its lines", t.Label())
	}
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
	m.setError(fmt.Errorf("thread %s is not shown in %s", t.Label(), m.viewLabel(v)))
}

// openWhereItWas shows where an outdated thread's lines were: in its file in the whole diff, or
// with !inDiff in its commit while the revision still has it, at the line nearest where the thread
// was, and says so. The thread itself is shown as it was only in the overview.
func (m *model) openWhereItWas(t review.ThreadView, inDiff bool) {
	loc := t.Location
	if loc.Path == "" {
		m.setError(fmt.Errorf("thread %s is outdated: what it is on changed", t.Label()))
		return
	}
	target := -1
	for i, v := range m.views {
		switch {
		case v.kind == viewDiff && target < 0, v.kind == viewDiff && inDiff:
			target = i
		case v.kind == viewCommit && !inDiff && v.commit.SHA == loc.Commit:
			target = i
		}
	}
	if target < 0 {
		m.setError(fmt.Errorf("thread %s is outdated, and there is no diff to show where it was", t.Label()))
		return
	}
	m.viewsNav.show(target, -1)
	m.openView()
	v := m.views[m.current]
	if m.needsLoad(v) {
		if !m.syncLoads {
			m.pendingThread, m.pendingInDiff = &t, inDiff
			return
		}
		m.data.apply(loadView(m.ctx, m.svc, m.data, v))
		m.views = buildViews(m.data)
		m.rebuildMain()
		v = m.views[m.current]
	}
	file := -1
	for f, d := range v.files {
		if d.NewPath == loc.Path || d.OldPath == loc.Path {
			file = f
		}
	}
	if file < 0 {
		m.setError(fmt.Errorf("thread %s is outdated, and %s is no longer in %s", t.Label(), loc.Path, m.viewLabel(v)))
		return
	}
	if m.collapsed(v, file) {
		m.toggleCollapsed(v, file)
	}
	at, best := -1, 0
	for i, r := range m.main.rows {
		if r.file != file {
			continue
		}
		if r.kind == rowFile && at < 0 {
			at = i
		}
		if n := lineOn(r, sideOr(loc.Side)); r.kind == rowLine && n > 0 {
			if d := abs(n - loc.StartLine); at < 0 || m.main.rows[at].kind != rowLine || d < best {
				at, best = i, d
			}
		}
	}
	m.main.jump(at)
	m.syncNav()
	m.setStatus("thread %s is outdated: its lines changed, and this is about where they were", t.Label())
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
			m.viewsNav.show(i, -1)
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
