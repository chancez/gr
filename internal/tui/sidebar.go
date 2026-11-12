package tui

import (
	"fmt"
	"strings"

	"github.com/chancez/gr/internal/tui/look"

	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
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
	return cutBack(s, width, 0)
}

// cutBack keeps width columns of s, ending shift columns before its end, marking each cut with
// "...".
func cutBack(s string, width, shift int) string {
	n := ansi.StringWidth(s)
	if n <= width {
		return s
	}
	if width <= 3 {
		return ansi.Truncate(s, width, "")
	}
	start := max(n-width-shift, 0)
	switch {
	case start == 0:
		return ansi.Truncate(s, width, "...")
	case start == n-width:
		return "..." + ansi.TruncateLeft(s, n-(width-3), "")
	case width <= 6:
		return ansi.Cut(s, start, start+width)
	}
	return "..." + ansi.Cut(s, start+3, start+width-3) + "..."
}

// navList is a scrolling list in the sidebar.
type navList struct {
	items []navItem
	pos   int
	top   int
	// summary, when set, is a line under the header, as the files' count and size.
	summary []look.Span
	// shift is how many columns the labels cut at the front are scrolled back toward their start.
	shift int
}

// sideStep is how many columns a sideways scroll of the sidebar's names moves them.
const sideStep = 4

// scrollSide scrolls the labels cut at the front n columns back toward their start, or toward their
// end when n is negative.
func (l *navList) scrollSide(n int) {
	l.shift = max(l.shift+n, 0)
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
// head is how many lines come before the list's entries: its header, and its summary, if any.
func (l *navList) head() int {
	if len(l.summary) > 0 {
		return 2
	}
	return 1
}

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
	rows := height - l.head()
	l.scrollTo(rows)
	// No further back than the longest label has to go to show its start.
	most := 0
	for _, it := range l.items {
		if it.prefix != "" {
			most = max(most, ansi.StringWidth(it.label)-it.labelRoom(th, width))
		}
	}
	l.shift = min(l.shift, most)
	out := []string{paneTitle(th, " "+title, width, focused)}
	if len(l.summary) > 0 && height > 1 {
		out = append(out, th.DrawCut(look.HeadingBar, width, l.summary...))
	}
	first, last := l.covers(l.top, rows)
	for y := range rows {
		i := l.top + y
		switch {
		case i >= len(l.items):
		case first && y == 0:
			// The indicator covers this entry, so it counts as hidden too.
			out = append(out, th.Draw(look.Plain, width, look.More(true, l.top+1)...))
		case last && y == rows-1:
			out = append(out, th.Draw(look.Plain, width, look.More(false, len(l.items)-i)...))
		default:
			out = append(out, l.items[i].render(th, width, l.shift, i == l.pos, focused))
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out
}

// width is the entry's width with nothing truncated.
func (it navItem) width(th *look.Theme) int {
	w := ansi.StringWidth(it.prefix + it.label)
	if it.sha != "" {
		w += ansi.StringWidth(th.TextOf(look.SHA(it.sha).Short())) + 1
	}
	if badge := th.TextOf(it.badge...); badge != "" {
		w += ansi.StringWidth(badge) + 1
	}
	return w
}

// room is the width left of the badge at width, and labelRoom the width of that for the label.
func (it navItem) room(th *look.Theme, width int) int {
	if badge := th.TextOf(it.badge...); badge != "" {
		return width - ansi.StringWidth(badge) - 1
	}
	return width
}

func (it navItem) labelRoom(th *look.Theme, width int) int {
	room := it.room(th, width) - ansi.StringWidth(it.prefix)
	if it.sha != "" {
		room -= ansi.StringWidth(th.TextOf(look.SHA(it.sha).Short())) + 1
	}
	return room
}

// render draws one entry at width: the label, truncated so the badge always shows at the right.
// A label cut at the front shows shift columns back from its end.
func (it navItem) render(th *look.Theme, width, shift int, selected, focused bool) string {
	room := it.room(th, width)
	label := ansi.Truncate(it.label, it.labelRoom(th, width), "...")
	if it.prefix != "" {
		label = it.prefix + cutBack(it.label, it.labelRoom(th, width), shift)
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
	if room == width {
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
		if to := m.data.interdiffTo; to.ID != m.data.revision.ID {
			return fmt.Sprintf("Rev %d to %d", m.data.interdiffFrom.Seq, to.Seq)
		}
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
	m.viewsNav.show(m.current, -1)

	keep := -1
	if m.filesNav.pos < len(m.filesNav.items) && m.filesNav.items[m.filesNav.pos].view == m.current {
		keep = m.filesNav.pos
	}
	v := m.views[m.current]
	counts := m.openThreads(v)
	m.filesNav.items = nil
	for fi, f := range v.files {
		// The message is the commit's header, not a file to list.
		if isMessage(f) {
			continue
		}
		badge := look.Badge(counts[f.Path()], m.draftThreads(v, f.Path()) > 0, m.fileReviewed(v, fi))
		if size, ok := diffSize(f); ok {
			// The file's size leads its badge, so it lines up at the right with the others.
			if len(badge) > 0 {
				size = append(size, look.Default(" "))
			}
			badge = append(size, badge...)
		}
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
			m.filesNav.show(m.current, i)
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
	if file := m.main.rows[m.main.cursor].file; file >= 0 {
		m.filesNav.show(m.current, file)
	}
}

// show selects the entry of file f of view v, or of view v itself when f is -1, and reports
// whether the list has one. Entries are found by what they show, never by index: not every view
// or file is listed, as the commit message is not, so an entry's index is no view's or file's.
func (l *navList) show(v, f int) bool {
	for i, it := range l.items {
		if it.view == v && it.file == f {
			l.pos = i
			return true
		}
	}
	return false
}

// sidebarHeights splits the sidebar between the views and files sections, header lines included.
// The sidebar sections are separated by one divider line.
const sidebarGap = 1

func (m *model) sidebarHeights(height int) (views, files int) {
	// Half each by default, and the file list there even when the view has no files, so the divider
	// stays put as views change.
	views = max(height/2, 2)
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
		m.filesNav.summary = m.filesSummary(width)
		files := m.filesNav.render(th, "Files", width, fh, m.focus == focusFiles)
		if len(m.filesNav.items) == 0 && len(files) > 1 {
			files[1] = th.DrawCut(look.Plain, width, look.Muted(" No files here"))
		}
		out = append(out, files...)
	}
	return out
}

// mainHeader titles the main pane with the view and the file under the cursor.
func (m *model) mainHeader(width int) string {
	v := m.views[m.current]
	label := " " + m.viewLabel(v) + " "
	var size, controls []look.Span
	switch {
	case v.kind == viewOverview:
		controls = m.controls("title", modeNormal, m.viewTarget(m.current, false), filterButton.labeled("Showing "+m.filter.String()))
	case v.kind == viewCommit:
		// The commit's header names it, with its size: the title says only where it is.
		at, of := 0, 0
		for i, w := range m.views {
			if w.kind == viewCommit {
				of++
				if i <= m.current {
					at++
				}
			}
		}
		label = fmt.Sprintf(" Commit %d of %d ", at, of)
		// Prev and Next, as GitHub's commit view has, greyed where there is no commit that way.
		controls = m.controls("title", modeNormal, theApp.Target(appSubject{}),
			prevCommit.disabledIf(at <= 1), nextCommit.disabledIf(at >= of))
	case v.kind != viewOverview && len(v.files) > 0:
		size = filesSize(v.files)
	}
	title := ""
	if m.needsLoad(v) {
		title += "  (loading)"
	}
	if c := m.main.cursor; c < len(m.main.rows) {
		r := m.main.rows[c]
		if r.file >= 0 && !isMessage(m.views[m.current].files[r.file]) {
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
	th, focused := m.r.th, m.focus == focusMain
	// The files' Prev and Next go before the file's name, whose length changes as the cursor moves.
	if prev, next, ok := m.fileSteps(); ok {
		steps := m.controls("title", modeNormal, theDiffPane.Target(paneSubject{}),
			prevFile.disabledIf(!prev), nextFile.disabledIf(!next))
		controls = append(append(controls, look.Default("  ")), steps...)
	}
	if size == nil && controls == nil {
		return m.withStatus(paneTitle(th, label+title, width, focused), width)
	}
	// The size keeps its colors on a chip of its own, as the bar draws its text in one color, and
	// the controls are drawn on the bar.
	chip := ""
	if size != nil {
		chip = th.Render(look.HeadingBar, append(append([]look.Span{look.Default(" ")}, size...), look.Default(" "))...)
	}
	if controls != nil {
		chip += th.Render(paneSurface(focused), append(append([]look.Span{look.Default(" ")}, controls...), look.Default(" "))...)
	}
	lead := paneTitle(th, label, min(ansi.StringWidth(label), width), focused)
	rest := max(width-ansi.StringWidth(lead)-ansi.StringWidth(chip), 0)
	return m.withStatus(lead+chip+paneTitle(th, title, rest, focused), width)
}

// fileSteps reports whether ] and [ have a file header to go to, after and before the cursor, for
// the title bar's Next and Prev file, and whether the shown diff has files to step through at all.
func (m *model) fileSteps() (prev, next, ok bool) {
	p := &m.main
	if m.views[m.current].kind == viewOverview {
		return false, false, false
	}
	for i, r := range p.rows {
		if r.kind != rowFile {
			continue
		}
		ok = true
		prev = prev || i < p.cursor
		next = next || i > p.cursor
	}
	return prev, next, ok
}

// filesSize is how many lines files add and delete in all.
func filesSize(files []git.FileDiff) []look.Span {
	added, deleted := 0, 0
	for _, f := range files {
		added, deleted = added+f.Added, deleted+f.Deleted
	}
	return look.DiffSize(added, deleted)
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
	return th.DrawCut(paneSurface(focused), width, look.Text(title))
}

// filesSummary is the line under the files list's header: how many files the view shown has, and
// their size in all, as the overview gives the change's, at the right as the entries' sizes are.
// Without files there is none.
func (m *model) filesSummary(width int) []look.Span {
	n := len(m.filesNav.items)
	if n == 0 {
		return nil
	}
	count := fmt.Sprintf(" %d files", n)
	if n == 1 {
		count = " 1 file"
	}
	var files []git.FileDiff
	v := m.views[m.current]
	for _, it := range m.filesNav.items {
		files = append(files, v.files[it.file])
	}
	size := filesSize(files)
	gap := width - ansi.StringWidth(count) - ansi.StringWidth(m.r.th.TextOf(size...))
	if gap < 1 {
		return []look.Span{look.Muted(count)}
	}
	return append([]look.Span{look.Muted(count), look.Default(strings.Repeat(" ", gap))}, size...)
}

// paneSurface is a pane's title bar, focused or not.
func paneSurface(focused bool) look.Surface {
	if focused {
		return look.PaneFocused
	}
	return look.PaneUnfocused
}

// sidebarAt maps a sidebar line to a list and its item index.
func (m *model) sidebarAt(y int) (*navList, focus, int) {
	l, f, line, _ := m.sidebarLine(y)
	return l, f, l.at(line)
}

// sidebarLine maps sidebar line y to a list, the line within its entries, and how many lines its
// entries have.
func (m *model) sidebarLine(y int) (l *navList, f focus, line, rows int) {
	_, _, bh := m.layoutSizes()
	vh, fh := m.sidebarHeights(bh)
	if y < vh {
		return &m.viewsNav, focusViews, y - m.viewsNav.head(), vh - m.viewsNav.head()
	}
	// Past the divider and the files header.
	return &m.filesNav, focusFiles, y - vh - sidebarGap - m.filesNav.head(), fh - m.filesNav.head()
}

// navPeek draws the sidebar entry under the mouse in full, when it is truncated at width, for the
// frame to draw over what is beside it. Nothing moves to make room for it.
func (m *model) navPeek(width int) (string, bool) {
	if !m.navHovering || width == 0 {
		return "", false
	}
	l, f, line, rows := m.sidebarLine(m.navHover)
	i := l.at(line)
	if i < 0 || l.indicatorAt(line, rows) != 0 {
		return "", false
	}
	it := l.items[i]
	w := min(it.width(m.r.th), m.width)
	if w <= width {
		return "", false
	}
	// Lit as the cursor is, so it stands apart from the diff it covers.
	return it.render(m.r.th, w, 0, true, i == l.pos && m.focus == f), true
}

// showFile shows file s in the main pane, in its view.
func (m *model) showFile(s fileSubject) {
	if s.at != m.current {
		m.setView(s.at)
	}
	for i, r := range m.main.rows {
		if r.kind == rowFile && r.file == s.file {
			m.main.jump(i)
			break
		}
	}
	m.syncNav()
}

// diffSize is how many lines file f adds and deletes, unless it has no lines to count: a binary
// file, or the commit message, which is not a file in git.
func diffSize(f git.FileDiff) ([]look.Span, bool) {
	if f.Binary || f.Path() == review.CommitMsgPath {
		return nil, false
	}
	return look.DiffSize(f.Added, f.Deleted), true
}
