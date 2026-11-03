package tui

import (
	"errors"
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/store"
)

// pickRevisions opens a picker of the change's revisions to compare from, newest first, then one
// of those after it to compare to, and shows the interdiff between the two.
func (m *model) pickRevisions() tea.Cmd {
	revs := m.data.revisions
	if len(revs) < 2 {
		m.setError(errors.New("the change has one revision; there is nothing to compare"))
		return nil
	}
	var items []pickerItem
	for _, r := range slices.Backward(revs[:len(revs)-1]) {
		items = append(items, m.revisionItem(r, func(m *model) tea.Cmd { return m.pickRevisionTo(r) }))
	}
	return m.openPicker(newPicker("Compare from revision", items, runItem(m)))
}

// pickRevisionTo opens a picker of the revisions after from, newest first, to compare from with.
func (m *model) pickRevisionTo(from store.Revision) tea.Cmd {
	var items []pickerItem
	for _, r := range slices.Backward(m.data.revisions) {
		if r.Seq > from.Seq {
			items = append(items, m.revisionItem(r, func(m *model) tea.Cmd {
				m.compareRevisions(from.Seq, r.Seq)
				return nil
			}))
		}
	}
	return m.openPicker(newPicker(fmt.Sprintf("Compare revision %d to", from.Seq), items, runItem(m)))
}

// revisionItem is a picker entry for revision r, noted with what it is to you.
func (m *model) revisionItem(r store.Revision, run func(*model) tea.Cmd) pickerItem {
	note := ""
	switch {
	case r.ID == m.data.revision.ID:
		note = "latest"
	case m.reviewedAt(r):
		note = "you reviewed it"
	}
	return pickerItem{
		value: fmt.Sprintf("rev %d  %s  %s", r.Seq, shortSHA(r.HeadSHA), r.CreatedAt.Local().Format("2006-01-02 15:04")),
		note:  note,
		run:   run,
	}
}

// reviewedAt reports whether you submitted a review of revision r.
func (m *model) reviewedAt(r store.Revision) bool {
	for _, rv := range m.data.reviews {
		if rv.Author == m.svc.Author() && rv.State == store.ReviewSubmitted && rv.RevisionID == r.ID {
			return true
		}
	}
	return false
}

// compareRevisions shows the interdiff from revision from to revision to, by seq, and keeps it
// across reloads.
func (m *model) compareRevisions(from, to int) {
	if !m.data.compare(from, to) {
		m.setError(fmt.Errorf("no revisions %d and %d to compare", from, to))
		return
	}
	m.comparing = &[2]int{from, to}
	m.views = buildViews(m.data)
	for i, v := range m.views {
		if v.kind == viewInterdiff {
			m.buildNav()
			m.viewsNav.show(i, -1)
			m.openView()
		}
	}
}
