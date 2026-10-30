package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
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
	switch msg.String() {
	case "o":
		m.fold(foldOpen)
	case "c":
		m.fold(foldClose)
	case "a":
		m.fold(foldToggle)
	case "R":
		m.foldAll(true)
	case "M":
		m.foldAll(false)
	}
	return m, nil
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
	p := &m.main
	if p.cursor >= len(p.rows) {
		return
	}
	v := m.views[m.current]
	r := p.rows[p.cursor]
	if r.kind == rowThread {
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
		m.rebuildKeeping(r)
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
		m.rebuildKeeping(r)
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

// foldAll opens or closes every fold in the view, files included.
func (m *model) foldAll(open bool) {
	v := m.views[m.current]
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

// rebuildKeeping rebuilds the main pane and puts the cursor back near the row it was on: the same
// row, the first line of an opened gap, or the gap a closed line was in.
func (m *model) rebuildKeeping(r row) {
	files := m.views[m.current].files
	y := m.main.cursorY()
	m.rebuildMain()
	for i, nr := range m.main.rows {
		same := nr.key(files) == r.key(files)
		if r.gap != nil && nr.gap != nil && nr.file == r.file && nr.gap.index == r.gap.index {
			same = true
		}
		if same {
			m.main.placeAt(i, y)
			return
		}
	}
	m.main.clamp()
}
