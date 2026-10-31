package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/store"
)

// openBase offers the repo's branches to move a branch change onto: its current base and the
// default branch first, then local branches, then remote ones.
func (m *model) openBase() tea.Cmd {
	c := m.data.change
	if c.Kind != store.KindBranch {
		m.setError(fmt.Errorf("change #%d is a %s, not a branch; it has no base branch", c.Seq, c.Kind))
		return nil
	}
	branches, err := m.svc.Git().Branches(m.ctx)
	if err != nil {
		m.setError(err)
		return nil
	}
	notes := map[string]string{c.BaseBranch: "current"}
	order := []string{c.BaseBranch}
	if def, err := m.svc.Git().DefaultBranch(m.ctx); err == nil && notes[def] == "" {
		notes[def] = "default"
		order = append(order, def)
	}
	var items []pickerItem
	for _, b := range order {
		items = append(items, pickerItem{value: b, note: notes[b]})
	}
	for _, b := range branches {
		if b != c.Branch && notes[b] == "" {
			items = append(items, pickerItem{value: b})
		}
	}
	svc, ctx := m.svc, m.ctx
	return m.openPicker(newPicker(fmt.Sprintf("Base branch for #%d %s", c.Seq, c.Branch), items, func(it pickerItem) tea.Cmd {
		if it.value == c.BaseBranch {
			return nil
		}
		return func() tea.Msg {
			_, _, err := svc.SetBase(ctx, c, it.value)
			return opMsg{status: fmt.Sprintf("change #%d moved from %s onto %s", c.Seq, c.BaseBranch, it.value), err: err, revised: true}
		}
	}))
}
