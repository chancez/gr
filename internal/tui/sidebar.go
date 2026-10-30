package tui

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/store"
)

// navItem is one sidebar entry: a view, or a file within the current view.
type navItem struct {
	label string
	view  int
	// file is the file index in the view, or -1 for the view itself.
	file int
	// prefix is shown before label and kept when label is truncated. File labels are truncated at
	// the front, so the file name stays visible.
	prefix string
}

// truncateLeft keeps the end of s, marking the cut with "...".
func truncateLeft(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	if width <= 3 {
		return ansi.Truncate(s, width, "")
	}
	return "..." + ansi.TruncateLeft(s, ansi.StringWidth(s)-(width-3), "")
}

// navList is a scrolling list in the sidebar.
type navList struct {
	items []navItem
	pos   int
	top   int
}

// move moves the selection by n, reporting whether it changed.
func (l *navList) move(n int) bool {
	prev := l.pos
	l.pos = min(max(l.pos+n, 0), max(len(l.items)-1, 0))
	return l.pos != prev
}

func (l *navList) set(i int) bool {
	return l.move(i - l.pos)
}

func (l *navList) scrollTo(height int) {
	if l.pos < l.top {
		l.top = l.pos
	} else if l.pos >= l.top+height {
		l.top = l.pos - height + 1
	}
	l.top = min(max(l.top, 0), max(len(l.items)-height, 0))
}

// at returns the item index on line y of the list, or -1.
func (l *navList) at(y int) int {
	i := l.top + y
	if y < 0 || i >= len(l.items) {
		return -1
	}
	return i
}

// render draws a titled section of height lines: a header, then items.
func (l *navList) render(st styles, title string, width, height int, focused bool) []string {
	if height <= 0 {
		return nil
	}
	header := st.navHeader.Render(fit(" "+title, width, nil))
	if focused {
		header = st.navFocused.Bold(true).Render(fit(" "+title, width, nil))
	} else {
		header = st.navSelected.Foreground(st.muted.GetForeground()).Bold(true).Render(fit(" "+title, width, nil))
	}
	out := []string{header}
	rows := height - 1
	l.scrollTo(rows)
	for i := l.top; i < len(l.items) && len(out) < height; i++ {
		it := l.items[i]
		label := ansi.Truncate(" "+it.label, width, "...")
		if it.prefix != "" {
			head := " " + it.prefix
			label = head + truncateLeft(it.label, width-ansi.StringWidth(head))
		}
		line := fit(label, width, nil)
		if i == l.pos {
			style := st.navSelected
			if focused {
				style = st.navFocused
			}
			line = style.Render(fit(ansi.Strip(label), width, nil))
		}
		out = append(out, line)
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

func (m *model) viewLabel(v view) string {
	switch v.kind {
	case viewOverview:
		return "Overview"
	case viewDiff:
		if len(v.files) == 1 {
			return "All changes (1 file)"
		}
		return fmt.Sprintf("All changes (%d files)", len(v.files))
	case viewCommit:
		return shortSHA(v.commit.SHA)[:7] + " " + v.commit.Subject()
	case viewInterdiff:
		return fmt.Sprintf("Since revision %d", m.data.interdiffFrom.Seq)
	}
	return ""
}

// openThreads counts unresolved current threads per file path in a view.
func (m *model) openThreads(v view) map[string]int {
	counts := map[string]int{}
	for i := range m.data.threads {
		t := &m.data.threads[i]
		if t.State != store.ThreadResolved && threadInView(t, v) {
			counts[t.Location.Path]++
		}
	}
	return counts
}

// buildNav fills both sidebar lists for the current view.
func (m *model) buildNav() {
	if m.data == nil {
		return
	}
	m.viewsNav.items = nil
	for vi, v := range m.views {
		m.viewsNav.items = append(m.viewsNav.items, navItem{label: m.viewLabel(v), view: vi, file: -1})
	}
	m.viewsNav.pos = m.current

	keep := -1
	if m.filesNav.pos < len(m.filesNav.items) && m.filesNav.items[m.filesNav.pos].view == m.current {
		keep = m.filesNav.pos
	}
	v := m.views[m.current]
	counts := m.openThreads(v)
	m.filesNav.items = nil
	for fi, f := range v.files {
		prefix := statusLetter(f.Status) + " "
		if n := counts[f.Path()]; n > 0 {
			prefix += fmt.Sprintf("(%d) ", n)
		}
		m.filesNav.items = append(m.filesNav.items, navItem{prefix: prefix, label: fileLabel(f), view: m.current, file: fi})
	}
	if keep >= 0 && keep < len(m.filesNav.items) {
		m.filesNav.pos = keep
	} else {
		m.filesNav.pos, m.filesNav.top = 0, 0
	}
}

// openView shows the view selected in the sidebar.
func (m *model) openView() {
	if m.viewsNav.pos < len(m.viewsNav.items) {
		m.setView(m.viewsNav.items[m.viewsNav.pos].view)
	}
}

// openFile scrolls the main pane to the file selected in the sidebar.
func (m *model) openFile() {
	if m.filesNav.pos >= len(m.filesNav.items) {
		return
	}
	file := m.filesNav.items[m.filesNav.pos].file
	for i, r := range m.main.rows {
		if r.kind == rowFile && r.file == file {
			m.main.jump(i)
			return
		}
	}
}

// syncNav selects the sidebar file under the main cursor.
func (m *model) syncNav() {
	if m.main.cursor >= len(m.main.rows) {
		return
	}
	if file := m.main.rows[m.main.cursor].file; file >= 0 && file < len(m.filesNav.items) {
		m.filesNav.pos = file
	}
}

// sidebarHeights splits the sidebar between the views and files sections, header lines included.
func (m *model) sidebarHeights(height int) (views, files int) {
	views = min(len(m.viewsNav.items)+1, max(4, height*2/5))
	if len(m.filesNav.items) == 0 {
		views = min(len(m.viewsNav.items)+1, height)
	}
	return views, height - views
}

func (m *model) renderSidebar(width, height int) []string {
	if width == 0 || m.data == nil {
		return nil
	}
	vh, fh := m.sidebarHeights(height)
	title := fmt.Sprintf("Change #%d", m.data.change.Seq)
	out := m.viewsNav.render(m.r.st, title, width, vh, m.focus == focusViews)
	if fh > 0 {
		out = append(out, m.filesNav.render(m.r.st, "Files", width, fh, m.focus == focusFiles)...)
	}
	return out
}

// mainHeader titles the main pane with the view and the file under the cursor.
func (m *model) mainHeader(width int) string {
	st := m.r.st
	title := " " + m.viewLabel(m.views[m.current])
	if c := m.main.cursor; c < len(m.main.rows) {
		if f := m.main.rows[c].file; f >= 0 {
			title += "  >  " + fileLabel(m.views[m.current].files[f])
		}
	}
	title = ansi.Truncate(title, width, "...")
	if m.focus == focusMain {
		return st.navFocused.Bold(true).Render(fit(title, width, nil))
	}
	return st.navSelected.Foreground(st.muted.GetForeground()).Bold(true).Render(fit(title, width, nil))
}
