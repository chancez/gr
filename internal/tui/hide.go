package tui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/ignore"
)

// fileHidden reports whether file f of view v is hidden from the review: hidden by hand, or by the
// filter unless shown by hand. The commit message is never hidden.
func (m *model) fileHidden(v view, f int) bool {
	if m.data == nil || isMessage(v.files[f]) {
		return false
	}
	path := v.files[f].Path()
	if hidden, ok := m.hidden[path]; ok {
		return hidden
	}
	return m.fileFilter.Hides(path)
}

// setHidden hides the files at paths from the review, or shows them, in every view.
func (m *model) setHidden(paths []string, hide bool) {
	for _, p := range paths {
		m.hidden[p] = hide
	}
	m.rebuildMain()
	m.buildNav()
	verb := map[bool]string{true: "hid", false: "showed"}[hide]
	if len(paths) == 1 {
		m.setStatus("%s %s", verb, paths[0])
	} else {
		m.setStatus("%s %d files", verb, len(paths))
	}
}

// unhideAll shows every file hidden from the review, by hand or by the filter.
func (m *model) unhideAll() {
	m.hidden, m.fileFilter = map[string]bool{}, ignore.Filter{}
	m.rebuildMain()
	m.buildNav()
	m.setStatus("showing every file")
}

// openFileFilter asks for the patterns that pick the files to review, starting from those in use.
// The status line's input is the search's, as the two never show at once.
func (m *model) openFileFilter() tea.Cmd {
	in := textinput.New()
	in.Prompt = "filter files: "
	in.Placeholder = "*.md !*_test.go"
	in.SetWidth(max(m.width-len(in.Prompt)-1, 10))
	in.SetValue(m.fileFilter.String())
	m.searchInput = in
	m.mode = modeFilter
	return m.searchInput.Focus()
}

func (m *model) fileFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case pressed(m, msg, actCancel, theApp.Target(appSubject{})):
		m.mode = modeNormal
		return m, nil
	case msg.String() == "enter":
		m.mode = modeNormal
		m.setFileFilter(ignore.ParseFilter(m.searchInput.Value()))
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

// setFileFilter hides the files f hides, as hiding each would: the files shown by hand are hidden
// again if f hides them, and those hidden by hand stay hidden.
func (m *model) setFileFilter(f ignore.Filter) {
	m.fileFilter = f
	for path, hidden := range m.hidden {
		if !hidden {
			delete(m.hidden, path)
		}
	}
	m.rebuildMain()
	m.buildNav()
	if f.String() == "" {
		m.setStatus("file filter cleared")
		return
	}
	v, n := m.views[m.current], 0
	for i := range v.files {
		if m.fileHidden(v, i) {
			n++
		}
	}
	m.setStatus("filter %s hides %d of %d files here", f, n, len(v.files))
}
