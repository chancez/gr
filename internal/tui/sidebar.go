package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
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
	// badge is right-aligned and kept when label is truncated: marks and thread counts.
	badge string
	// heading styles entries that are not commits or files, such as the overview, so they read as
	// peers of the commit list rather than parents of it.
	heading bool
	// done dims an entry that is marked reviewed, so finished work recedes at a glance.
	done bool
}

// colorBadge colors a badge's parts: the open thread count, the draft mark, and the reviewed
// mark.
func colorBadge(st styles, badge string) string {
	var parts []string
	for _, p := range strings.Fields(badge) {
		switch {
		case strings.HasPrefix(p, "("):
			parts = append(parts, lipgloss.NewStyle().Foreground(st.accent).Render(p))
		default:
			var b strings.Builder
			for _, r := range p {
				switch string(r) {
				case markReviewed:
					b.WriteString(st.badgeOpen.Bold(true).Render(markReviewed))
				case markDraft:
					b.WriteString(st.badgeWarn.Bold(true).Render(markDraft))
				default:
					b.WriteRune(r)
				}
			}
			parts = append(parts, b.String())
		}
	}
	return strings.Join(parts, " ")
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
func (l *navList) render(st styles, title string, width, height int, focused bool) []string {
	if height <= 0 {
		return nil
	}
	rows := height - 1
	l.scrollTo(rows)
	out := []string{paneTitle(st, " "+title, width, focused)}
	first, last := l.covers(l.top, rows)
	for y := range rows {
		i := l.top + y
		switch {
		case i >= len(l.items):
		case first && y == 0:
			// The indicator covers this entry, so it counts as hidden too.
			out = append(out, st.muted.Render(fit(fmt.Sprintf("^ %d more", l.top+1), width, nil)))
		case last && y == rows-1:
			out = append(out, st.muted.Render(fit(fmt.Sprintf("v %d more", len(l.items)-i), width, nil)))
		default:
			out = append(out, l.items[i].render(st, width, i == l.pos, focused))
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// render draws one entry at width: the label, truncated so the badge always shows at the right.
func (it navItem) render(st styles, width int, selected, focused bool) string {
	room := width
	if it.badge != "" {
		room -= ansi.StringWidth(it.badge) + 1
	}
	label := ansi.Truncate(it.label, room, "...")
	if it.prefix != "" {
		label = it.prefix + truncateLeft(it.label, room-ansi.StringWidth(it.prefix))
	}
	text := fit(label, room, nil)
	badge := ""
	if it.badge != "" {
		badge = " " + it.badge
	}
	// Selected and heading entries take one style over the whole line, so their badge is plain.
	switch {
	case selected && focused:
		return st.navFocused.Render(fit(text+badge, width, nil))
	case selected:
		return st.navSelected.Render(fit(text+badge, width, nil))
	case it.heading:
		return st.navHeading.Render(fit(text+badge, width, nil))
	}
	if it.done {
		text = st.muted.Render(text)
	}
	if badge != "" {
		badge = " " + colorBadge(st, it.badge)
	}
	return fit(text+badge, width, nil)
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
		if m.data.interdiffFromReview {
			return fmt.Sprintf("Since your review (rev %d)", m.data.interdiffFrom.Seq)
		}
		return fmt.Sprintf("Since rev %d", m.data.interdiffFrom.Seq)
	}
	return ""
}

// Sidebar marks, shown in an entry's right-aligned badge.
//
// TODO: make these configurable, so icons such as checkboxes can replace the ASCII defaults.
const (
	// markReviewed is shown on files and commits marked reviewed.
	markReviewed = "x"
	// markDraft is shown where you have draft comments not yet submitted.
	markDraft = "*"
)

// marks renders the marks that apply, drafts first, or "" when none do.
func marks(reviewed, draft bool) string {
	var out string
	if draft {
		out += markDraft
	}
	if reviewed {
		out += markReviewed
	}
	return out
}

// draftThreads counts the viewer's draft threads in a view, on one path or, if path is empty, on
// any.
func (m *model) draftThreads(v view, path string) int {
	n := 0
	for i := range m.data.threads {
		t := &m.data.threads[i]
		if m.data.isDraft(t.ReviewID) && threadInView(t, v) && (path == "" || t.Location.Path == path) {
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
		it := navItem{label: m.viewLabel(v), view: vi, file: -1, heading: v.kind != viewCommit}
		if v.kind == viewCommit || v.kind == viewDiff {
			it.done = m.viewReviewed(v)
			it.badge = marks(it.done, m.draftThreads(v, "") > 0)
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
		badge := marks(m.fileReviewed(v, fi), m.draftThreads(v, f.Path()) > 0)
		if n := counts[f.Path()]; n > 0 {
			badge = strings.TrimSpace(fmt.Sprintf("(%d) %s", n, badge))
		}
		m.filesNav.items = append(m.filesNav.items, navItem{
			prefix: statusLetter(f.Status) + " ", label: fileLabel(f), badge: badge, view: m.current, file: fi,
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
	out := m.viewsNav.render(m.r.st, title, width, vh, m.focus == focusViews)
	if fh > 0 {
		out = append(out, m.r.st.paneBorder.Render(strings.Repeat("-", width)))
		out = append(out, m.filesNav.render(m.r.st, "Files", width, fh, m.focus == focusFiles)...)
	}
	return out
}

// mainHeader titles the main pane with the view and the file under the cursor.
func (m *model) mainHeader(width int) string {
	st := m.r.st
	title := " " + m.viewLabel(m.views[m.current])
	if c := m.main.cursor; c < len(m.main.rows) {
		r := m.main.rows[c]
		if r.file >= 0 {
			f := m.views[m.current].files[r.file]
			title += "  >  " + fileLabel(f)
			if r.hunk != nil {
				for i := range f.Hunks {
					if &f.Hunks[i] == r.hunk {
						title += fmt.Sprintf("  (hunk %d/%d)", i+1, len(f.Hunks))
					}
				}
			}
		}
	}
	return m.withStatus(paneTitle(st, title, width, m.focus == focusMain), width)
}

// withStatus overlays the current message at the right of the main pane's title bar, so it does
// not hide the key hints. The title gives up room first.
func (m *model) withStatus(title string, width int) string {
	if m.status == "" {
		return title
	}
	st := m.r.st
	msg := ansi.Truncate(m.status, max(width/2, 20), "...")
	style := st.statusMsg
	if m.statusErr {
		style = st.statusErrMsg
	}
	badge := style.Render(" " + msg + " ")
	keep := max(width-ansi.StringWidth(badge), 0)
	return ansi.Truncate(title, keep, "") + badge
}

// paneTitle draws a pane's title bar.
func paneTitle(st styles, title string, width int, focused bool) string {
	style := st.paneUnfocused
	if focused {
		style = st.paneFocused
	}
	return style.Render(fit(ansi.Truncate(title, width, "..."), width, nil))
}
