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
	if m.mode != modeNormal || msg.Button != tea.MouseLeft {
		return m, nil
	}
	nw, mw, bh := m.layoutSizes()
	if msg.Y >= bh {
		return m, nil
	}
	if nw > 0 && msg.X == nw {
		// The divider: drag to resize the sidebar.
		m.dragging = true
		return m, nil
	}
	if vh, fh := m.sidebarHeights(bh); msg.X < nw && fh > 0 && msg.Y == vh {
		// The divider between the commit list and the file list: drag to resize them.
		m.draggingSplit = true
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
		m.setFocus(f)
		if i >= 0 {
			l.pos = i
			if f == focusViews {
				m.openView()
			} else {
				m.openFile()
			}
		}
		return m, nil
	}
	m.setFocus(focusMain)
	i := m.main.rowAt(msg.Y - 1)
	if i < 0 || !m.main.rows[i].selectable() {
		return m, nil
	}
	m.main.cursor = i
	if m.main.reader {
		m.main.selectAt(m.main.offset + msg.Y - 1)
	}
	if m.split && m.main.rows[i].kind == rowLine {
		if msg.X-nw-1 < mw/2 {
			m.main.side = anchor.SideOld
		} else {
			m.main.side = anchor.SideNew
		}
	}
	m.syncNav()
	return m, nil
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
