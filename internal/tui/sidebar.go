package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/store"
)

// navItem is one sidebar entry: a view, or a file within the current view.
type navItem struct {
	// sha is a commit's short sha, shown before label.
	sha   string
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

// badgeSpans are a badge's parts: the open thread count, the draft mark, and the reviewed mark.
func badgeSpans(badge string) []span {
	var out []span
	for i, p := range strings.Fields(badge) {
		if i > 0 {
			out = append(out, sp(elDefault, " "))
		}
		if strings.HasPrefix(p, "(") {
			out = append(out, sp(elCount, p))
			continue
		}
		for _, r := range p {
			switch string(r) {
			case markReviewed:
				out = append(out, sp(elReviewed, markReviewed, strong))
			case markDraft:
				out = append(out, sp(elDraft, markDraft, strong))
			default:
				out = append(out, sp(elDefault, string(r)))
			}
		}
	}
	return out
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
func (l *navList) render(th *theme, title string, width, height int, focused bool) []string {
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
			out = append(out, th.draw(surfPlain, width, sp(elMuted, fmt.Sprintf("^ %d more", l.top+1))))
		case last && y == rows-1:
			out = append(out, th.draw(surfPlain, width, sp(elMuted, fmt.Sprintf("v %d more", len(l.items)-i))))
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
func (it navItem) render(th *theme, width int, selected, focused bool) string {
	room := width
	if it.badge != "" {
		room -= ansi.StringWidth(it.badge) + 1
	}
	lead := 0
	if it.sha != "" {
		lead = ansi.StringWidth(it.sha) + 1
	}
	label := ansi.Truncate(it.label, room-lead, "...")
	if it.prefix != "" {
		label = it.prefix + truncateLeft(it.label, room-ansi.StringWidth(it.prefix))
	}
	surf, el := surfPlain, elDefault
	var p props
	switch {
	case selected && focused:
		surf, el = surfCursor, elText
	case selected:
		surf, el = surfCursorDim, elText
	case it.heading:
		surf, el = surfHeading, elHeading
	case it.done:
		p = dim
	}
	var spans []span
	if it.sha != "" {
		spans = append(spans, sp(elSHA, it.sha, p), sp(el, " ", p))
	}
	out := th.draw(surf, room, append(spans, sp(el, label, p))...)
	if it.badge == "" {
		return out
	}
	return out + th.draw(surf, width-room, append([]span{sp(el, " ")}, badgeSpans(it.badge)...)...)
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
			it.sha, it.label = shortSHA(v.commit.SHA)[:7], v.commit.Subject()
		}
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
		out = append(out, th.render(surfPlain, sp(elBorder, strings.Repeat("-", width))))
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
	return m.withStatus(paneTitle(m.r.th, title, width, m.focus == focusMain), width)
}

// withStatus overlays the current message at the right of the main pane's title bar, so it does
// not hide the key hints. The title gives up room first.
func (m *model) withStatus(title string, width int) string {
	if m.status == "" {
		return title
	}
	msg := ansi.Truncate(m.status, max(width/2, 20), "...")
	surf := surfStatus
	if m.statusErr {
		surf = surfStatusError
	}
	badge := m.r.th.render(surf, sp(elText, " "+msg+" "))
	keep := max(width-ansi.StringWidth(badge), 0)
	return ansi.Truncate(title, keep, "") + badge
}

// paneTitle draws a pane's title bar.
func paneTitle(th *theme, title string, width int, focused bool) string {
	surf := surfPaneUnfocused
	if focused {
		surf = surfPaneFocused
	}
	return th.drawCut(surf, width, sp(elText, title))
}
