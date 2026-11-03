package tui

// jumpPoint is a place to come back to: a view, the row in it, and where on screen the row was.
type jumpPoint struct {
	view string
	row  string
	seg  int
	y    int
}

// maxJumps bounds the jump list, as vim's does.
const maxJumps = 100

func (m *model) here() jumpPoint {
	p := jumpPoint{view: m.views[m.current].key(), y: m.main.cursorY(), seg: m.main.itemSegment()}
	if m.main.cursor < len(m.main.rows) {
		p.row = m.main.rows[m.main.cursor].key(m.views[m.current].files)
	}
	return p
}

// jump runs a move and, if it changed the place, records where it started in the jump list. A move
// that goes nowhere, such as t with no thread ahead, records nothing.
func (m *model) jump(move func()) {
	if m.data == nil || m.restoring {
		move()
		return
	}
	from := m.here()
	move()
	if m.here() != from {
		m.pushJump(from)
	}
}

// pushJump records a place. Jumping from the middle of the list drops the places ahead of it, as a
// new branch of history, and a place equal to the last one is not recorded twice.
func (m *model) pushJump(p jumpPoint) {
	m.jumps = m.jumps[:m.jumpAt]
	if n := len(m.jumps); n > 0 && m.jumps[n-1] == p {
		m.jumpAt = n
		return
	}
	m.jumps = append(m.jumps, p)
	if len(m.jumps) > maxJumps {
		m.jumps = m.jumps[len(m.jumps)-maxJumps:]
	}
	m.jumpAt = len(m.jumps)
}

// jumpBack goes to the previous place in the jump list, saving the current one first so jumpForward
// can return to it.
func (m *model) jumpBack() {
	if m.jumpAt == len(m.jumps) {
		m.jumps = append(m.jumps, m.here())
	}
	if m.jumpAt == 0 {
		m.setStatus("at the oldest jump")
		return
	}
	m.jumpAt--
	m.restore(m.jumps[m.jumpAt])
}

// jumpForward goes to the next place in the jump list.
func (m *model) jumpForward() {
	if m.jumpAt+1 >= len(m.jumps) {
		m.setStatus("at the newest jump")
		return
	}
	m.jumpAt++
	m.restore(m.jumps[m.jumpAt])
}

func (m *model) restore(p jumpPoint) {
	m.restoring = true
	defer func() { m.restoring = false }()
	for i, v := range m.views {
		if v.key() == p.view {
			m.viewsNav.show(i, -1)
			m.openView()
		}
	}
	files := m.views[m.current].files
	for i, r := range m.main.rows {
		if r.key(files) == p.row {
			m.main.placeAt(i, p.seg, p.y)
			m.syncNav()
			return
		}
	}
}
