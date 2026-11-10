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

// Text in a diff's code is selected with the mouse: a drag from one cell to another, across lines,
// over the code alone, not its line numbers or signs. Letting go copies it, through the terminal,
// and leaves its lines selected, for c to comment on them.

// codePos is a cell of a diff line's code: its row, the side of a split diff, or a unified line,
// and the column in the code as drawn, tabs expanded.
type codePos struct {
	row  int
	side string
	col  int
}

// before reports whether p comes before q in the text.
func (p codePos) before(q codePos) bool {
	return p.row < q.row || p.row == q.row && p.col < q.col
}

// textDrag is code text selected with the mouse, from anchor to head, and whether the button is
// still down.
type textDrag struct {
	anchor codePos
	head   codePos
	down   bool
}

// span is the selection in order.
func (t *textDrag) span() (codePos, codePos) {
	if t.head.before(t.anchor) {
		return t.head, t.anchor
	}
	return t.anchor, t.head
}

// codeRow is the name of row i's code, for its zone.
func codeRow(i int) string { return "code" + strconv.Itoa(i) }

// codeAt is the code cell at x, y in the last frame, if code is drawn there.
func (m *model) codeAt(x, y int) (codePos, bool) {
	for id, zones := range m.zones {
		row, side, ok := parseCodeZone(id)
		if !ok {
			continue
		}
		for _, z := range zones {
			if z.Contains(x, y) {
				return codePos{row: row, side: side, col: x - z.X}, true
			}
		}
	}
	return codePos{}, false
}

// codeOnLine is the code cell on screen line y nearest x, on side, for a drag to reach: before
// the code its start, past it its end.
func (m *model) codeOnLine(x, y int, side string) (codePos, bool) {
	for id, zones := range m.zones {
		row, s, ok := parseCodeZone(id)
		if !ok || s != side {
			continue
		}
		for _, z := range zones {
			if z.Y == y {
				return codePos{row: row, side: side, col: min(max(x-z.X, 0), z.W)}, true
			}
		}
	}
	return codePos{}, false
}

func parseCodeZone(id string) (int, string, bool) {
	rest, ok := strings.CutPrefix(id, "code")
	if !ok {
		return 0, "", false
	}
	n, side, ok := strings.Cut(rest, ":")
	row, err := strconv.Atoi(n)
	return row, side, ok && err == nil
}

// textActive reports whether text is selected: being dragged over, or since, while the lines it
// selected are selected. It is kept as the view scrolls, and let go of as the cursor moves by key.
func (m *model) textActive() bool {
	t := m.text
	if t == nil || t.anchor == t.head {
		return false
	}
	return t.down || m.main.anchor == t.anchor.row
}

// textOnRow is where the selection is on row i, in columns of its code: from, to, up to the
// line's end where it runs on.
func (m *model) textOnRow(i int) (from, to int, ok bool) {
	if !m.textActive() {
		return 0, 0, false
	}
	return m.text.onRow(i)
}

// onRow is where the selection is on row i, whether or not it is still shown.
func (t *textDrag) onRow(i int) (from, to int, ok bool) {
	lo, hi := t.span()
	if i < lo.row || i > hi.row {
		return 0, 0, false
	}
	from, to = 0, 1<<30
	if i == lo.row {
		from = lo.col
	}
	if i == hi.row {
		to = hi.col
	}
	return from, to, true
}

// paintText draws the selection on row i's drawn line, over its code.
func (m *model) paintText(i int, line string) string {
	from, to, ok := m.textOnRow(i)
	if !ok {
		return line
	}
	_, zones := look.Scan(line)
	z, found := zones[codeZone(codeRow(i), m.text.anchor.side)]
	if !found || len(z) == 0 {
		return line
	}
	start, end := z[0].X+min(from, z[0].W), z[0].X+min(to, z[0].W)
	if end <= start {
		return line
	}
	style := m.r.th.SelectionStyle()
	return look.KeepMarks(line, func(l string) string { return lipgloss.StyleRanges(l, lipgloss.NewRange(start, end, style)) })
}

// textPress starts a selection at x, y when it is on code, and drops any before.
func (m *model) textPress(x, y int) {
	if m.textActive() {
		// The lines it selected go with it.
		m.main.anchor = -1
	}
	m.text = nil
	if pos, ok := m.codeAt(x, y); ok {
		m.text = &textDrag{anchor: pos, head: pos, down: true}
	}
}

// textDragTo moves the selection's head to where the mouse is, along the code of the line it is
// on, on the side the selection began on, and in its file, as a comment on its lines can be.
func (m *model) textDragTo(x, y int) {
	pos, ok := m.codeOnLine(x, y, m.text.anchor.side)
	if ok && m.main.rows[pos.row].file == m.main.rows[m.text.anchor.row].file {
		m.text.head = pos
	}
}

// textLines are the first and last rows text selected with the mouse is on, while it is.
func (m *model) textLines() (lo, hi int, ok bool) {
	if !m.textActive() {
		return 0, 0, false
	}
	from, to := m.text.span()
	return from.row, to.row, true
}

// textRelease ends the drag: a selection is copied, and its lines selected, for c to comment on.
// A press with no drag selects nothing.
func (m *model) textRelease() tea.Cmd {
	t := m.text
	t.down = false
	if t.anchor == t.head {
		m.text = nil
		return nil
	}
	text, lines := m.selectedText()
	m.main.anchor, m.main.cursor = t.anchor.row, t.head.row
	m.syncNav()
	plural := "lines"
	if lines == 1 {
		plural = "line"
	}
	m.setStatus("copied %d %s; c comments on them", lines, plural)
	return tea.SetClipboard(text)
}

// selectedText is the code selected, as the lines have it, tabs and all, and how many lines it is
// on.
func (m *model) selectedText() (string, int) {
	lo, hi := m.text.span()
	var out []string
	for i := lo.row; i <= hi.row && i < len(m.main.rows); i++ {
		l, ok := m.lineOn(i, lo.side)
		if !ok {
			continue
		}
		from, to, _ := m.text.onRow(i)
		text := l.Text
		out = append(out, text[cellIndex(text, from):cellIndex(text, to)])
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
