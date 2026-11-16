package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/tui/look"
)

// Text is selected with the mouse: a drag from one cell to another, across lines, over a diff's
// code alone, not its line numbers or signs, or over a comment's body. Letting go copies it,
// through the terminal. Code leaves its lines selected, for c to comment on them.

// textPos is a cell of text that can be selected: its row; the side of a split diff, a unified
// line, or the comment it is in; the line of the comment's body as drawn; and the column as drawn,
// a diff's tabs expanded.
type textPos struct {
	row  int
	side string
	line int
	col  int
}

// before reports whether p comes before q in the text.
func (p textPos) before(q textPos) bool {
	if p.row != q.row {
		return p.row < q.row
	}
	if p.line != q.line {
		return p.line < q.line
	}
	return p.col < q.col
}

// inComment reports whether p is in a comment's body rather than a diff's code.
func (p textPos) inComment() bool { return strings.HasPrefix(p.side, "comment") }

// textDrag is text selected with the mouse, from anchor to head, and whether the button is still
// down.
type textDrag struct {
	anchor textPos
	head   textPos
	down   bool
}

// span is the selection in order.
func (t *textDrag) span() (textPos, textPos) {
	if t.head.before(t.anchor) {
		return t.head, t.anchor
	}
	return t.anchor, t.head
}

// textRow is the name of row i's text, for its zones.
func textRow(i int) string { return "text" + strconv.Itoa(i) }

// commentZone is the zone line k of comment c's body is drawn in, under the row's name.
func commentZone(row string, c, k int) string {
	return row + ":comment" + strconv.Itoa(c) + ":" + strconv.Itoa(k)
}

// textAt is the cell of text at x, y in the last frame, if text that can be selected is drawn
// there.
func (m *model) textAt(x, y int) (textPos, bool) {
	for id, zones := range m.zones {
		pos, ok := parseTextZone(id)
		if !ok {
			continue
		}
		for _, z := range zones {
			if z.Contains(x, y) {
				pos.col = x - z.X
				return pos, true
			}
		}
	}
	return textPos{}, false
}

// textOnLine is the cell on screen line y nearest x of the text anchor is in, for a drag to
// reach: code on its side, or its comment. Before the text it is its start, past it its end.
func (m *model) textOnLine(x, y int, anchor textPos) (textPos, bool) {
	for id, zones := range m.zones {
		pos, ok := parseTextZone(id)
		if !ok || pos.side != anchor.side || anchor.inComment() && pos.row != anchor.row {
			continue
		}
		for _, z := range zones {
			if z.Y == y {
				pos.col = min(max(x-z.X, 0), z.W)
				return pos, true
			}
		}
	}
	return textPos{}, false
}

// parseTextZone is where the text zone id names: its row, side, and line.
func parseTextZone(id string) (textPos, bool) {
	rest, ok := strings.CutPrefix(id, "text")
	parts := strings.Split(rest, ":")
	if !ok || len(parts) < 2 || len(parts) > 3 {
		return textPos{}, false
	}
	row, err := strconv.Atoi(parts[0])
	if err != nil {
		return textPos{}, false
	}
	pos := textPos{row: row, side: parts[1]}
	if len(parts) == 3 {
		if pos.line, err = strconv.Atoi(parts[2]); err != nil {
			return textPos{}, false
		}
	}
	return pos, true
}

// textActive reports whether text is selected: being dragged over, or since. Code stays while
// the lines it selected are selected, and a comment's text until a click or a key lets it go. It
// is kept as the view scrolls, and let go of as the cursor moves by key.
func (m *model) textActive() bool {
	t := m.text
	if t == nil || t.anchor == t.head {
		return false
	}
	return t.down || t.anchor.inComment() || m.main.anchor == t.anchor.row
}

// textRows are the first and last rows text selected with the mouse is on, while it is.
func (m *model) textRows() (lo, hi int, ok bool) {
	if !m.textActive() {
		return 0, 0, false
	}
	from, to := m.text.span()
	return from.row, to.row, true
}

// on is where the selection is on line k of row i, in columns of its text: from, to, up to the
// line's end where it runs on. It holds whether or not the selection is still shown.
func (t *textDrag) on(i, k int) (from, to int, ok bool) {
	lo, hi := t.span()
	at := textPos{row: i, line: k}
	if at.before(textPos{row: lo.row, line: lo.line}) || (textPos{row: hi.row, line: hi.line}).before(at) {
		return 0, 0, false
	}
	from, to = 0, 1<<30
	if i == lo.row && k == lo.line {
		from = lo.col
	}
	if i == hi.row && k == hi.line {
		to = hi.col
	}
	return from, to, true
}

// paintText draws the selection on row i's drawn lines, over its text.
func (m *model) paintText(i int, lines []string) []string {
	if lo, hi, ok := m.textRows(); !ok || i < lo || i > hi {
		return lines
	}
	style := m.r.th.SelectionStyle()
	for k, line := range lines {
		_, zones := look.Scan(line)
		for id, zs := range zones {
			pos, ok := parseTextZone(id)
			if !ok || pos.row != i || pos.side != m.text.anchor.side || len(zs) == 0 {
				continue
			}
			from, to, ok := m.text.on(i, pos.line)
			start, end := zs[0].X+min(from, zs[0].W), zs[0].X+min(to, zs[0].W)
			if !ok || end <= start {
				continue
			}
			lines[k] = look.KeepMarks(lines[k], func(l string) string {
				return lipgloss.StyleRanges(l, lipgloss.NewRange(start, end, style))
			})
		}
	}
	return lines
}

// textPress starts a selection at x, y when it is on text that can be selected, and drops any
// before.
func (m *model) textPress(x, y int) {
	if m.textActive() && !m.text.anchor.inComment() {
		// The lines it selected go with it.
		m.main.anchor = -1
	}
	m.text = nil
	if pos, ok := m.textAt(x, y); ok {
		m.text = &textDrag{anchor: pos, head: pos, down: true}
	}
}

// textDragTo moves the selection's head to where the mouse is, along the text of the line it is
// on: on the side code began on, and in its file, as a comment on its lines can be, or in the
// comment it began in.
func (m *model) textDragTo(x, y int) {
	pos, ok := m.textOnLine(x, y, m.text.anchor)
	if ok && m.main.rows[pos.row].file == m.main.rows[m.text.anchor.row].file {
		m.text.head = pos
	}
}

// textLines are the first and last rows code selected with the mouse is on, while it is.
func (m *model) textLines() (lo, hi int, ok bool) {
	if m.text == nil || m.text.anchor.inComment() {
		return 0, 0, false
	}
	return m.textRows()
}

// textRelease ends the drag: a selection is copied, and code's lines selected, for c to comment
// on. A press with no drag selects nothing.
func (m *model) textRelease() tea.Cmd {
	t := m.text
	t.down = false
	if t.anchor == t.head {
		m.text = nil
		return nil
	}
	text, lines := m.selectedText()
	plural := "lines"
	if lines == 1 {
		plural = "line"
	}
	if t.anchor.inComment() {
		m.setStatus("copied %d %s", lines, plural)
		return tea.SetClipboard(text)
	}
	m.main.anchor, m.main.cursor = t.anchor.row, t.head.row
	m.syncNav()
	m.setStatus("copied %d %s; c comments on them", lines, plural)
	return tea.SetClipboard(text)
}

// selectedText is the text selected, and how many lines it is on: code as the lines have it, tabs
// and all, and a comment as drawn.
func (m *model) selectedText() (string, int) {
	lo, hi := m.text.span()
	if lo.inComment() {
		return m.commentText()
	}
	var out []string
	for i := lo.row; i <= hi.row && i < len(m.main.rows); i++ {
		l, ok := m.lineOn(i, lo.side)
		if !ok {
			continue
		}
		from, to, _ := m.text.on(i, 0)
		text := l.Text
		out = append(out, text[cellIndex(text, from):cellIndex(text, to)])
	}
	return strings.Join(out, "\n"), len(out)
}

// commentText is the comment's text selected, as its row draws it, without the spaces that pad
// its lines.
func (m *model) commentText() (string, int) {
	a := m.text.anchor
	var out []string
	for _, line := range m.renderMainRow(a.row, rowState{}) {
		plain, zones := look.Scan(line)
		plain = ansi.Strip(plain)
		for id, zs := range zones {
			pos, ok := parseTextZone(id)
			if !ok || pos.row != a.row || pos.side != a.side || len(zs) == 0 {
				continue
			}
			from, to, ok := m.text.on(a.row, pos.line)
			if !ok {
				continue
			}
			z := zs[0]
			out = append(out, strings.TrimRight(ansi.Cut(plain, z.X+min(from, z.W), z.X+min(to, z.W)), " "))
		}
	}
	return strings.Join(out, "\n"), len(out)
}

// lineOn is row i's diff line on side, if it has one there.
func (m *model) lineOn(i int, side string) (git.Line, bool) {
	rw := m.main.rows[i]
	if rw.kind != rowLine {
		return git.Line{}, false
	}
	switch {
	case side == sideUnified && rw.line != nil:
		return *rw.line, true
	case side == "old" && rw.left != nil:
		return *rw.left, true
	case side == "new" && rw.right != nil:
		return *rw.right, true
	}
	return git.Line{}, false
}

// cellIndex is the byte index in text of the character drawn at cell col, its tabs drawn as
// expandTabs draws them: past the end, the end.
func cellIndex(text string, col int) int {
	cells := 0
	for i, r := range text {
		w := ansi.StringWidth(string(r))
		if r == '\t' {
			w = len(expandTabs("\t"))
		}
		if cells+w > col {
			return i
		}
		cells += w
	}
	return len(text)
}
