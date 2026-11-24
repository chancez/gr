package tui

import (
	"errors"
	"maps"
	"slices"

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
	return m.pathHidden(v.files[f].Path())
}

// pathHidden reports whether the file at path is hidden from the review, as fileHidden does.
func (m *model) pathHidden(path string) bool {
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

// startPicking shows the file list's checkboxes, to check files to act on together.
func (m *model) startPicking() {
	m.picking = true
	m.setFocus(focusFiles)
	m.buildNav()
}

// stopPicking unchecks every file, and puts the checkboxes away.
func (m *model) stopPicking() {
	m.picking, m.picked = false, map[string]bool{}
	m.buildNav()
}

// togglePicked checks or unchecks the file at path, showing the checkboxes if they are not.
func (m *model) togglePicked(path string) {
	m.setPicked(path, !m.picked[path])
}

// setPicked checks the file at path, or unchecks it, showing the checkboxes if they are not.
func (m *model) setPicked(path string, on bool) {
	m.picking = true
	if on {
		m.picked[path] = true
	} else {
		delete(m.picked, path)
	}
	m.buildNav()
}

// pickDrag is a drag over the file list's checkboxes: what it makes the boxes it passes over,
// checked or unchecked as the press made the first, and the entry it is at.
type pickDrag struct {
	on bool
	at int
}

// dragPick carries a drag over the file list's checkboxes to entry i, setting every box from the
// entry it was at, as the mouse can pass over some between two reports.
func (m *model) dragPick(i int) {
	d := m.pickDrag
	if i < 0 || i >= len(m.filesNav.items) || i == d.at {
		return
	}
	step := 1
	if i < d.at {
		step = -1
	}
	for j := d.at + step; ; j += step {
		if path, ok := m.pickable(focusFiles, j); ok {
			m.setPicked(path, d.on)
		}
		if j == i {
			break
		}
	}
	d.at, m.filesNav.pos = i, i
}

// pickable is the path of the file at entry i of sidebar list f, if it has a checkbox now: while
// files are being selected, every file listed has one, the hidden ones too, to unhide them.
func (m *model) pickable(f focus, i int) (string, bool) {
	if f != focusFiles || !m.picking || i < 0 || i >= len(m.filesNav.items) {
		return "", false
	}
	it := m.filesNav.items[i]
	if it.file < 0 {
		return "", false
	}
	return m.views[it.view].files[it.file].Path(), true
}

// pickAll checks every file the shown view lists with a checkbox: all but the hidden ones.
func (m *model) pickAll() {
	v := m.views[m.current]
	for i, f := range v.files {
		if !isMessage(f) && !m.fileHidden(v, i) {
			m.picked[f.Path()] = true
		}
	}
	m.buildNav()
}

// hidePicked hides the checked files, and puts the checkboxes away.
func (m *model) hidePicked() {
	shown, _ := m.pickedFiles()
	if len(shown) == 0 {
		m.setError(errors.New("no shown files selected; x selects one"))
		return
	}
	m.picking, m.picked = false, map[string]bool{}
	m.setHidden(shown, true)
}

// unhidePicked shows the checked files that are hidden, and puts the checkboxes away.
func (m *model) unhidePicked() {
	_, hidden := m.pickedFiles()
	if len(hidden) == 0 {
		m.setError(errors.New("no hidden files selected; x selects one under the hidden entry"))
		return
	}
	m.picking, m.picked = false, map[string]bool{}
	m.setHidden(hidden, false)
}

// pickedFiles are the paths of the checked files, those shown and those hidden.
func (m *model) pickedFiles() (shown, hidden []string) {
	for _, p := range slices.Sorted(maps.Keys(m.picked)) {
		if m.pathHidden(p) {
			hidden = append(hidden, p)
		} else {
			shown = append(shown, p)
		}
	}
	return shown, hidden
}
