package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/tui/interact"
)

// Menus and the palette list a target's actions in the picker, by subject.

// keyFor is how the key shows that runs a on chain, or "".
func (m *model) keyFor(chain []interact.Target[*model], a interact.Bound[*model]) string {
	for _, k := range interact.Keys(m, chain...) {
		if k.Action.Same(a) {
			return k.Help().Key
		}
	}
	return ""
}

// actionItems are a target's actions that apply, as picker entries. A menu groups them under a
// heading per subject, when there is more than one; flat, for the palette, each names its subject.
func (m *model) actionItems(t interact.Target[*model], flat bool) []pickerItem {
	groups := t.Groups(m)
	chain := m.chainOf(t)
	var items []pickerItem
	for _, g := range groups {
		if !flat && len(groups) > 1 {
			items = append(items, pickerItem{value: g.Label, heading: true})
		}
		for _, a := range g.Actions {
			value, full := a.Title, g.Label+": "+a.Title
			if flat {
				value = full
			}
			// An entry shows the key that runs it here, if one does.
			items = append(items, pickerItem{value: value, full: full, note: m.keyFor(chain, a), run: func(m *model) tea.Cmd {
				if !a.Applies(m) {
					return nil
				}
				return a.Run(m)
			}})
		}
	}
	return items
}

func runItem(m *model) func(pickerItem) tea.Cmd {
	return func(it pickerItem) tea.Cmd {
		if it.run == nil {
			return nil
		}
		return it.run(m)
	}
}

// openMenu opens the menu of what t's subject can do where it is, with its top left corner just
// right of x, y, where it was clicked.
func (m *model) openMenu(t interact.Target[*model], x, y int) tea.Cmd {
	items := m.actionItems(t, false)
	if len(items) == 0 {
		return nil
	}
	// One subject's menu is titled by it; several are each under a divider.
	title := "Actions"
	if g := t.Groups(m); len(g) == 1 {
		title = g[0].Label
	}
	p := newPicker(title, items, runItem(m))
	p.anchored, p.anchorX, p.anchorY, p.bound = true, x, y, true
	return m.openPicker(p)
}

// openPalette opens every action of the cursor's subjects, to find one by its title.
func (m *model) openPalette() tea.Cmd {
	t, ok := m.cursorTarget()
	if !ok {
		return nil
	}
	items := m.actionItems(t, true)
	if len(items) == 0 {
		return nil
	}
	p := newPicker("Actions here", items, runItem(m))
	p.bound = true
	return m.openPicker(p)
}

// closeStaleMenus closes a menu of actions when new data arrives: its subjects are the rows they
// were on, and the rows are built again.
func (m *model) closeStaleMenus() {
	if m.mode == modePicker && m.picker.bound {
		m.closePicker()
	}
}
