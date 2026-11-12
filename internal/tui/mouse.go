package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/tui/interact"
)

func (m *model) mouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.mode == modePicker {
		m.pickerWheel(msg)
		return m, nil
	}
	nw, _, _ := m.layoutSizes()
	delta := 3
	if msg.Button == tea.MouseWheelUp {
		delta = -3
	} else if msg.Button != tea.MouseWheelDown {
		return m, nil
	}
	if msg.X < nw {
		l, _, _ := m.sidebarAt(msg.Y)
		l.top = max(l.top+delta, 0)
		return m, nil
	}
	if m.main.reader {
		m.main.readerWheel(delta)
	} else {
		m.main.scroll(delta)
	}
	// What is under the mouse moved with the scroll.
	m.hoverAt(msg.X, msg.Y)
	m.syncNav()
	return m, nil
}

func (m *model) mouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.mode == modePicker && msg.Button == tea.MouseLeft {
		return m.pickerClick(msg)
	}
	// A right click with a menu open opens the menu of where it lands instead.
	if m.mode == modePicker && msg.Button == tea.MouseRight {
		m.closePicker()
	}
	// A control runs on a left click, in any mode it is drawn in.
	if msg.Button == tea.MouseLeft {
		if c, ok := m.controlAt(msg.X, msg.Y); ok && c.run.Applies(m) {
			if c.selects {
				m.selectFrom(msg.X, msg.Y, c.run)
				return m, nil
			}
			return m, c.run.Run(m)
		}
	}
	// The search bar, on the status line, places its cursor where it is clicked.
	if _, _, bh := m.layoutSizes(); m.mode == modeSearch && msg.Button == tea.MouseLeft && msg.Y == bh {
		placeCursor(&m.searchInput, msg.X)
		return m, nil
	}
	// In the comment box's text, a press places the cursor, and a drag from it selects.
	if m.mode == modeCompose && msg.Button == tea.MouseLeft && !m.compose.closing && m.composePress(msg.X, msg.Y) {
		return m, nil
	}
	// A left click moves the cursor to what it lands on, as keys would. A right click opens the
	// menu of what is there, and moves nothing.
	right := msg.Button == tea.MouseRight
	if m.mode != modeNormal || (msg.Button != tea.MouseLeft && !right) {
		return m, nil
	}
	// A press on code starts selecting its text, for a drag to carry on; the click still moves
	// the cursor, as below.
	if !right {
		m.textPress(msg.X, msg.Y)
	}
	// A mention, such as a sha in a thread's location, never takes the cursor: any click on it
	// opens its menu.
	if t, ok := m.mentionAt(msg.X, msg.Y); ok {
		return m, m.openMenu(t, msg.X, msg.Y)
	}
	nw, _, bh := m.layoutSizes()
	if msg.Y >= bh {
		return m, nil
	}
	// Titles, dividers, and indicators have no menu, so a right click on one does nothing.
	if right && msg.X < nw && msg.Y == 0 {
		return m, nil
	}
	if nw > 0 && msg.X == nw {
		// The divider: drag to resize the sidebar.
		m.dragging = !right
		return m, nil
	}
	if vh, fh := m.sidebarHeights(bh); msg.X < nw && fh > 0 && msg.Y == vh {
		// The divider between the commit list and the file list: drag to resize them.
		m.draggingSplit = !right
		return m, nil
	}
	if msg.X < nw {
		l, f, i := m.sidebarAt(msg.Y)
		vh, fh := m.sidebarHeights(bh)
		rows, y := vh-l.head(), msg.Y-l.head()
		if f == focusFiles {
			rows, y = fh-l.head(), msg.Y-vh-sidebarGap-l.head()
		}
		if dir := l.indicatorAt(y, rows); dir != 0 {
			if right {
				return m, nil
			}
			// An indicator of more entries: move the selection a page that way.
			m.setFocus(f)
			l.move(dir * max(rows-2, 1))
			if f == focusViews {
				m.openView()
			} else {
				m.openFile()
			}
			return m, nil
		}
		if !right {
			m.setFocus(f)
		}
		if i < 0 {
			return m, nil
		}
		// A right click asks what the entry can do, and leaves the selection where it was.
		if right {
			if t, ok := m.sidebarEntry(f, i); ok {
				return m, m.openMenu(t, msg.X, msg.Y)
			}
			return m, nil
		}
		l.pos = i
		return m, m.landed(true)
	}
	i := m.main.rowAt(msg.Y - m.mainTop())
	if h, ok := m.stickyFile(); ok && msg.Y == m.mainTop() {
		// The pinned header is its file's, whatever row it covers.
		i = h
	}
	if i < 0 || !m.main.rows[i].selectable() {
		if !right {
			m.setFocus(focusMain)
		}
		return m, nil
	}
	// A right click asks what the row can do, and leaves the cursor where it was.
	if right {
		side := m.main.side
		if m.split {
			side = m.sideAt(msg.X)
		}
		if t, ok := m.mainRowTarget(i, side); ok {
			return m, m.openMenu(t, msg.X, msg.Y)
		}
		return m, nil
	}
	m.setFocus(focusMain)
	m.main.cursor = i
	if m.main.reader {
		m.main.selectAt(m.main.offset + msg.Y - m.mainTop())
	}
	if m.split && m.main.rows[i].kind == rowLine {
		m.main.side = m.sideAt(msg.X)
	}
	m.syncNav()
	return m, nil
}

// sideAt is the side of a split diff at column x.
func (m *model) sideAt(x int) anchor.Side {
	nw, mw, _ := m.layoutSizes()
	if x-nw-1 < mw/2 {
		return anchor.SideOld
	}
	return anchor.SideNew
}

// selecting is a line selection the mouse is dragging, from a control that runs on it when the
// button is let go.
type selecting struct {
	run interact.Bound[*model]
}

// selectFrom starts a line selection at the line at x, y, the press on a control that selects, as
// the + that comments on a line.
func (m *model) selectFrom(x, y int, run interact.Bound[*model]) {
	i := m.main.rowAt(y - m.mainTop())
	if i < 0 {
		return
	}
	m.setFocus(focusMain)
	if lo, hi, ok := m.textLines(); ok && i >= lo && i <= hi {
		// On a line of text selected with the mouse, the + comments on all the lines it is on, on
		// its side.
		m.main.anchor, m.main.cursor = lo, hi
		if m.split {
			m.main.side = anchor.Side(m.text.anchor.side)
		}
	} else {
		m.main.cursor, m.main.anchor = i, i
		if m.split {
			m.main.side = m.sideAt(x)
		}
	}
	m.selecting = &selecting{run: run}
	m.syncNav()
}

// selectTo moves the dragged selection's end to the line at y, within the file it started in.
func (m *model) selectTo(y int) {
	p := &m.main
	i := p.rowAt(y - m.mainTop())
	if i < 0 || p.rows[i].kind != rowLine || p.rows[i].file != p.rows[p.anchor].file {
		return
	}
	p.cursor = i
	m.syncNav()
}

// selected ends the dragged selection: what it started from runs on the lines selected, through the
// target of the line the drag ended on, as its key would there.
func (m *model) selected() tea.Cmd {
	s := m.selecting
	m.selecting = nil
	t, ok := m.mainRowTarget(m.main.cursor, m.main.side)
	if !ok {
		return nil
	}
	for _, g := range t.Groups(m) {
		for _, b := range g.Actions {
			if b.Same(s.run) {
				cmd := b.Run(m)
				m.compose.mouseLines = m.mode == modeCompose
				return cmd
			}
		}
	}
	return nil
}

func (m *model) mouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeCompose && m.compose.dragging {
		m.composeDrag(msg.X, msg.Y)
		return m, nil
	}
	if m.text != nil && m.text.down {
		m.textDragTo(msg.X, msg.Y)
		return m, nil
	}
	if m.selecting != nil {
		m.selectTo(msg.Y)
		return m, nil
	}
	if m.draggingSplit {
		m.viewsHeight = max(msg.Y, 2)
		return m, nil
	}
	if !m.dragging {
		m.hoverAt(msg.X, msg.Y)
		return m, nil
	}
	m.navWidth = min(max(msg.X, 12), m.width-20)
	m.relayout(false)
	return m, nil
}

// hoverAt notes the main pane's row under the mouse at x, y, which shows the controls the mouse
// can reach there, as the + that comments on a line.
func (m *model) hoverAt(x, y int) {
	p := &m.main
	nw, _, bh := m.layoutSizes()
	p.hovering = false
	if m.data == nil || x <= nw || y < m.mainTop() || y >= bh {
		return
	}
	i := p.rowAt(y - m.mainTop())
	if h, ok := m.stickyFile(); ok && y == m.mainTop() {
		i = h
	}
	if i < 0 {
		return
	}
	p.hovering, p.hover = true, i
	if m.split {
		p.hoverSide = m.sideAt(x)
	}
}
