package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/review"
)

type foldOp int

const (
	foldOpen foldOp = iota
	foldClose
	foldToggle
)

// pendingKey finishes a two-key command.
func (m *model) pendingKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	prefix := m.pending
	m.pending = ""
	m.status = ""
	if prefix != "z" {
		return m, nil
	}
	c, _ := m.runOn(msg, theFoldPrefix.Target(paneSubject{}))
	return m, c
}

// fileContent returns a file's new-side lines in a view, reading them once.
func (m *model) fileContent(v view, f int) []string {
	path := v.files[f].NewPath
	k := v.newRev + "\x00" + path
	if lines, ok := m.contents[k]; ok {
		return lines
	}
	lines, err := m.svc.FileLines(m.ctx, v.newRev, path)
	if err != nil {
		m.setError(err)
		return nil
	}
	m.contents[k] = lines
	return lines
}

// fold opens or closes the fold under the cursor: a gap of unchanged lines, or the file.
func (m *model) fold(op foldOp) {
	m.foldRow(m.main.cursor, op)
}

// foldRow opens or closes the fold of row i: a thread, a gap of unchanged lines, or the file.
func (m *model) foldRow(i int, op foldOp) {
	p := &m.main
	if i < 0 || i >= len(p.rows) {
		return
	}
	v := m.views[m.current]
	r := p.rows[i]
	if (r.kind == rowThread || r.kind == rowReview) && r.thread != nil {
		collapsed := m.threadCollapsed(*r.thread)
		switch op {
		case foldOpen:
			collapsed = false
		case foldClose:
			collapsed = true
		case foldToggle:
			collapsed = !collapsed
		}
		m.threadFolds[r.thread.ID] = collapsed
		// The cursor's row stays where it is on screen, so a click on another thread's chevron
		// moves nothing above it.
		m.rebuildKeeping(p.rows[p.cursor])
		return
	}
	if r.gap != nil {
		k := gapKey(v, v.files[r.file].Path(), r.gap.index)
		open := m.openGaps[k]
		switch op {
		case foldOpen:
			open = true
		case foldClose:
			open = false
		case foldToggle:
			open = !open
		}
		if open {
			m.openGaps[k] = true
		} else {
			delete(m.openGaps, k)
		}
		m.rebuildKeeping(p.rows[p.cursor])
		return
	}
	if r.file < 0 {
		m.setError(errors.New("no fold here"))
		return
	}
	collapsed := m.collapsed(v, r.file)
	want := collapsed
	switch op {
	case foldOpen:
		want = false
	case foldClose:
		want = true
	case foldToggle:
		want = !collapsed
	}
	if want != collapsed {
		m.toggleCollapsed(v, r.file)
		m.focusFile(r.file)
	}
}

// foldAll opens or closes every fold in the view: in the overview its threads, and in a diff its
// files and gaps.
func (m *model) foldAll(open bool) {
	v := m.views[m.current]
	if v.kind == viewOverview {
		m.foldThreads(open, func(review.ThreadView) bool { return true })
		return
	}
	for _, file := range v.files {
		m.expanded[v.key()+"\x00"+file.Path()] = open
		for _, g := range gaps(file) {
			k := gapKey(v, file.Path(), g.index)
			if open {
				m.openGaps[k] = true
			} else {
				delete(m.openGaps, k)
			}
		}
	}
	var cur row
	if m.main.cursor < len(m.main.rows) {
		cur = m.main.rows[m.main.cursor]
	}
	m.rebuildKeeping(cur)
	m.buildNav()
}

// foldIn opens or closes the fold under the cursor and every fold inside it: on a review's bar, its
// summary and all its threads.
func (m *model) foldIn(open bool) {
	p := &m.main
	if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowReview && p.rows[p.cursor].review != nil {
		id := p.rows[p.cursor].review.ID
		m.foldThreads(open, func(t review.ThreadView) bool { return t.ReviewID == id })
		return
	}
	op := foldClose
	if open {
		op = foldOpen
	}
	m.fold(op)
}

// foldThreads opens or closes every thread that matches, keeping the cursor where it was.
func (m *model) foldThreads(open bool, match func(review.ThreadView) bool) {
	for _, t := range m.data.threads {
		if match(t) {
			m.threadFolds[t.ID] = !open
		}
	}
	var cur row
	if m.main.cursor < len(m.main.rows) {
		cur = m.main.rows[m.main.cursor]
	}
	m.rebuildKeeping(cur)
}

// rebuildKeeping rebuilds the main pane and puts the cursor back near the row it was on: the same
// row, the first line of an opened gap, or the gap a closed line was in.
func (m *model) rebuildKeeping(r row) {
	files := m.views[m.current].files
	y := m.main.cursorY()
	seg := m.main.itemSegment()
	m.rebuildMain()
	for i, nr := range m.main.rows {
		same := nr.key(files) == r.key(files)
		if r.gap != nil && nr.gap != nil && nr.file == r.file && nr.gap.index == r.gap.index {
			same = true
		}
		if same {
			m.main.placeAt(i, seg, y)
			return
		}
	}
	m.main.clamp()
}
