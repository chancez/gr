package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/store"
)

// pickChange opens a picker of the open changes, then the branches with no change yet, to switch
// to without leaving gr. A branch picked gets a change onto the default branch.
func (m *model) pickChange() tea.Cmd {
	return m.changePicker("Switch to a change", (*model).switchChange)
}

// changePicker opens a picker of the open changes, then the branches with no change yet, titled
// title, and runs open on the one picked. A branch picked gets a change onto the default branch.
func (m *model) changePicker(title string, open func(m *model, c store.Change) tea.Cmd) tea.Cmd {
	changes, err := m.svc.ListChanges(m.ctx, store.ChangeOpen)
	if err != nil {
		m.setError(err)
		return nil
	}
	items := []pickerItem{{value: "Changes", heading: true}}
	reviewed := map[string]bool{}
	for _, c := range changes {
		note := ""
		if c.ID == m.changeID {
			note = "current"
		}
		if c.Kind == store.KindBranch {
			reviewed[c.Branch] = true
		}
		items = append(items, pickerItem{value: changeLabel(c), note: note, run: func(m *model) tea.Cmd {
			return open(m, c)
		}})
	}
	branches, err := m.svc.Git().Branches(m.ctx)
	if err != nil {
		m.setError(err)
		return nil
	}
	def, _ := m.svc.Git().DefaultBranch(m.ctx)
	first := true
	for _, b := range branches {
		if reviewed[b] || b == def {
			continue
		}
		if first {
			items = append(items, pickerItem{value: "Branches", heading: true})
			first = false
		}
		items = append(items, pickerItem{value: b, note: "new change", run: func(m *model) tea.Cmd {
			return m.reviewBranch(b, open)
		}})
	}
	return m.openPicker(newPicker(title, items, runItem(m)))
}

// changeLabel names a change in the picker: its number, and its branch and base or its title.
func changeLabel(c store.Change) string {
	if c.Kind == store.KindBranch {
		return fmt.Sprintf("#%d %s onto %s", c.Seq, c.Branch, c.BaseBranch)
	}
	return fmt.Sprintf("#%d %s", c.Seq, c.Title)
}

// reviewBranch finds or makes the change for branch b, onto the default branch, and opens it.
func (m *model) reviewBranch(b string, open func(m *model, c store.Change) tea.Cmd) tea.Cmd {
	t, err := m.svc.ParseTarget(m.ctx, b, "")
	if err != nil {
		m.setError(err)
		return nil
	}
	c, _, err := m.svc.EnsureChange(m.ctx, t)
	if err != nil {
		m.setError(err)
		return nil
	}
	return open(m, c)
}

// switchChange shows change c in place of the one shown: everything about the old change is
// dropped, and how gr looks, its theme, panes, and split, is kept.
func (m *model) switchChange(c store.Change) tea.Cmd {
	if c.ID == m.changeID {
		return nil
	}
	m.savePlace()
	m.changeID = c.ID
	m.data, m.views, m.current = nil, nil, 0
	m.viewsNav, m.filesNav = navList{}, navList{}
	m.search, m.searchPos, m.searchHits = "", 0, 0
	m.pendingLoads, m.pendingThread, m.pendingFile, m.comparing = nil, nil, "", nil
	m.jumps, m.jumpAt, m.returns = nil, 0, map[viewKind]jumpPoint{}
	m.threadFolds, m.openGaps, m.expanded = map[string]bool{}, map[string]bool{}, map[string]bool{}
	m.contents, m.segments = map[string][]string{}, map[int][]int{}
	scrolloff, height := m.main.scrolloff, m.main.height
	m.main = pane{anchor: -1, side: m.main.side, scrolloff: scrolloff, height: height}
	m.pendingPlace, m.savedPlace = nil, nil
	if p, ok := m.readPlace(c); ok {
		m.savedPlace = &p
	}
	m.setStatus("switched to %s", changeLabel(c))
	return m.loadCmd()
}
