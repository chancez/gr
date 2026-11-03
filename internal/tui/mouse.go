package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/chancez/gr/internal/anchor"
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
	// A left click moves the cursor to what it lands on, as keys would. A right click opens the
	// menu of what is there, and moves nothing.
	right := msg.Button == tea.MouseRight
	if m.mode != modeNormal || (msg.Button != tea.MouseLeft && !right) {
		return m, nil
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
		rows, y := vh-1, msg.Y-1
		if f == focusFiles {
			rows, y = fh-1, msg.Y-vh-sidebarGap-1
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
	i := m.main.rowAt(msg.Y - 1)
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
		m.main.selectAt(m.main.offset + msg.Y - 1)
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

func (m *model) mouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if m.draggingSplit {
		m.viewsHeight = max(msg.Y, 2)
		return m, nil
	}
	if !m.dragging {
		return m, nil
	}
	m.navWidth = min(max(msg.X, 12), m.width-20)
	m.relayout(false)
	return m, nil
}
