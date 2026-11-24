package tui

import (
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
