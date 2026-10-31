package tui

import (
	"fmt"
	"strings"

	"github.com/chancez/gr/internal/tui/look"

	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/store"
)

// navItem is one sidebar entry: a view, or a file within the current view.
type navItem struct {
	// sha is a commit's sha, shown short before label.
	sha   string
	label string
	view  int
	// file is the file index in the view, or -1 for the view itself.
	file int
	// prefix is shown before label and kept when label is truncated. File labels are truncated at
	// the front, so the file name stays visible.
	prefix string
	// badge is right-aligned and kept when label is truncated: marks and thread counts.
	badge []look.Span
	// heading styles entries that are not commits or files, such as the overview, so they read as
	// peers of the commit list rather than parents of it.
	heading bool
	// done dims an entry that is marked reviewed, so finished work recedes at a glance.
	done bool
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

// covers reports whether the first and last of height rows, starting at top, are covered by an
// indicator of entries scrolled out of view.
func (l *navList) covers(top, height int) (first, last bool) {
	return top > 0, top+height < len(l.items)
}

// scrollTo scrolls so the selection shows within height rows, clear of the indicator lines.
func (l *navList) scrollTo(height int) {
	for range 3 {
		first, last := l.covers(l.top, height)
		switch {
		case first && l.pos <= l.top:
			// Leave the indicator a row above the selection, or none at the very top.
			l.top = max(l.pos-1, 0)
		case !first && l.pos < l.top:
			l.top = l.pos
		case last && l.pos >= l.top+height-1:
			l.top = l.pos - height + 2
		case !last && l.pos >= l.top+height:
			l.top = l.pos - height + 1
		default:
			l.top = min(max(l.top, 0), max(len(l.items)-height, 0))
			return
		}
		l.top = min(max(l.top, 0), max(len(l.items)-height, 0))
	}
}

// indicatorAt reports whether line y of the list is an indicator: -1 for the one above, 1 below.
func (l *navList) indicatorAt(y, height int) int {
	first, last := l.covers(l.top, height)
	switch {
	case first && y == 0:
		return -1
	case last && y == height-1:
		return 1
	}
	return 0
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
func (l *navList) render(th *look.Theme, title string, width, height int, focused bool) []string {
	if height <= 0 {
		return nil
	}
	rows := height - 1
	l.scrollTo(rows)
	out := []string{paneTitle(th, " "+title, width, focused)}
	first, last := l.covers(l.top, rows)
	for y := range rows {
		i := l.top + y
		switch {
		case i >= len(l.items):
		case first && y == 0:
			// The indicator covers this entry, so it counts as hidden too.
			out = append(out, th.Draw(look.Plain, width, look.Muted(fmt.Sprintf("^ %d more", l.top+1))))
		case last && y == rows-1:
			out = append(out, th.Draw(look.Plain, width, look.Muted(fmt.Sprintf("v %d more", len(l.items)-i))))
		default:
			out = append(out, l.items[i].render(th, width, i == l.pos, focused))
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// render draws one entry at width: the label, truncated so the badge always shows at the right.
func (it navItem) render(th *look.Theme, width int, selected, focused bool) string {
	room := width
	badge := look.TextOf(it.badge...)
	if badge != "" {
		room -= ansi.StringWidth(badge) + 1
	}
	lead := 0
	if it.sha != "" {
		lead = ansi.StringWidth(look.TextOf(look.SHA(it.sha).Short())) + 1
	}
	label := ansi.Truncate(it.label, room-lead, "...")
	if it.prefix != "" {
		label = it.prefix + truncateLeft(it.label, room-ansi.StringWidth(it.prefix))
	}
	surf, text := look.Plain, look.Default
	var p []look.Prop
	switch {
	case selected && focused:
		surf, text = look.Cursor, look.Text
	case selected:
		surf, text = look.CursorDim, look.Text
	case it.heading:
		surf, text = look.HeadingBar, look.Heading
	case it.done:
		p = []look.Prop{look.Dim}
	}
	var spans []look.Span
	if it.sha != "" {
		spans = append(spans, look.SHA(it.sha).Short().With(p...), text(" ").With(p...))
	}
	out := th.Draw(surf, room, append(spans, text(label).With(p...))...)
	if badge == "" {
		return out
	}
	return out + th.Draw(surf, width-room, append([]look.Span{text(" ")}, it.badge...)...)
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
		return shortSHA(v.commit.SHA) + " " + v.commit.Subject()
	case viewInterdiff:
		if m.data.interdiffFromReview {
			return fmt.Sprintf("Since your review (rev %d)", m.data.interdiffFrom.Seq)
		}
		return fmt.Sprintf("Since rev %d", m.data.interdiffFrom.Seq)
	}
	return ""
}

// draftThreads counts the viewer's draft threads in a view, on one path or, if path is empty, on
// any.
func (m *model) draftThreads(v view, path string) int {
	n := 0
	for i := range m.data.threads {
		t := &m.data.threads[i]
		if loc, ok := m.data.locationIn(t, v); ok && m.data.isDraft(t.ReviewID) && (path == "" || loc.Path == path) {
			n++
		}
	}
	return n
}

// openThreads counts unresolved current threads per file path in a view.
func (m *model) openThreads(v view) map[string]int {
	counts := map[string]int{}
	for i := range m.data.threads {
		t := &m.data.threads[i]
		if loc, ok := m.data.locationIn(t, v); ok && t.State != store.ThreadResolved {
			counts[loc.Path]++
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
		it := navItem{label: m.viewLabel(v), view: vi, file: -1, heading: v.kind != viewCommit}
		if v.kind == viewCommit {
			it.sha, it.label = v.commit.SHA, v.commit.Subject()
		}
		if v.kind == viewCommit || v.kind == viewDiff {
			it.done = m.viewReviewed(v)
			it.badge = look.Badge(0, m.draftThreads(v, "") > 0, it.done)
		}
		m.viewsNav.items = append(m.viewsNav.items, it)
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
		badge := look.Badge(counts[f.Path()], m.draftThreads(v, f.Path()) > 0, m.fileReviewed(v, fi))
		m.filesNav.items = append(m.filesNav.items, navItem{
			prefix: look.StatusLetter(f.Status) + " ", label: look.FileLabel(f), badge: badge, view: m.current, file: fi,
			done: m.fileReviewed(v, fi),
		})
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

// openFileByPath scrolls the main pane to a file of the current view.
func (m *model) openFileByPath(path string) {
	for i, f := range m.views[m.current].files {
		if f.Path() == path {
			for j, n := range m.filesNav.items {
				if n.file == i {
					m.filesNav.pos = j
				}
			}
			m.openFile()
			return
		}
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
// The sidebar sections are separated by one divider line.
const sidebarGap = 1

func (m *model) sidebarHeights(height int) (views, files int) {
	if len(m.filesNav.items) == 0 {
		return min(len(m.viewsNav.items)+1, height), 0
	}
	views = min(len(m.viewsNav.items)+1, max(4, height*2/5))
	if m.viewsHeight > 0 {
		// Dragged: the commit list keeps the height it was given, leaving the files a header and a row.
		views = min(m.viewsHeight, max(height-sidebarGap-2, 2))
	}
	return views, max(height-views-sidebarGap, 0)
}

func (m *model) renderSidebar(width, height int) []string {
	if width == 0 || m.data == nil {
		return nil
	}
	vh, fh := m.sidebarHeights(height)
	title := fmt.Sprintf("Change #%d", m.data.change.Seq)
	th := m.r.th
	out := m.viewsNav.render(th, title, width, vh, m.focus == focusViews)
	if fh > 0 {
		out = append(out, th.Render(look.Plain, look.Border(strings.Repeat("-", width), false)))
		out = append(out, m.filesNav.render(th, "Files", width, fh, m.focus == focusFiles)...)
	}
	return out
}

// mainHeader titles the main pane with the view and the file under the cursor.
func (m *model) mainHeader(width int) string {
	title := " " + m.viewLabel(m.views[m.current])
	if m.needsLoad(m.views[m.current]) {
		title += "  (loading)"
	}
	if c := m.main.cursor; c < len(m.main.rows) {
		r := m.main.rows[c]
		if r.file >= 0 {
			f := m.views[m.current].files[r.file]
			title += "  >  " + look.FileLabel(f)
			if r.hunk != nil {
				for i := range f.Hunks {
					if &f.Hunks[i] == r.hunk {
						title += fmt.Sprintf("  (hunk %d/%d)", i+1, len(f.Hunks))
					}
				}
			}
		}
	}
	return m.withStatus(paneTitle(m.r.th, title, width, m.focus == focusMain), width)
}

// withStatus overlays the current message at the right of the main pane's title bar, so it does
// not hide the key hints. The title gives up room first.
func (m *model) withStatus(title string, width int) string {
	if m.status == "" {
		return title
	}
	msg := ansi.Truncate(m.status, max(width/2, 20), "...")
	surf := look.Status
	if m.statusErr {
		surf = look.StatusError
	}
	badge := m.r.th.Render(surf, look.Text(" "+msg+" "))
	keep := max(width-ansi.StringWidth(badge), 0)
	return ansi.Truncate(title, keep, "") + badge
}

// paneTitle draws a pane's title bar.
func paneTitle(th *look.Theme, title string, width int, focused bool) string {
	surf := look.PaneUnfocused
	if focused {
		surf = look.PaneFocused
	}
	return th.DrawCut(surf, width, look.Text(title))
}
