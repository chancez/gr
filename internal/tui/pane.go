package tui

import (
	"sort"

	"github.com/chancez/gr/internal/git"

	"github.com/chancez/gr/internal/anchor"
)

// pane is the main pane's scrollable list of rows. Line rows are one display line each and are
// rendered only when visible. Other rows are rendered up front to learn their height.
type pane struct {
	rows   []row
	starts []int
	total  int
	cache  map[int][]string
	cursor int
	// anchor is the other end of a line selection, or -1.
	anchor int
	offset int
	width  int
	height int
	side   anchor.Side
	// scrolloff is the number of lines kept between the cursor and the pane's edges, as in vim.
	scrolloff int
}

// defaultScrolloff is the scrolloff panes start with.
const defaultScrolloff = 5

// margin is the scrolloff that fits: at most a third of the pane each side.
func (p *pane) margin() int { return max(min(p.scrolloff, (p.height-1)/3), 0) }

func (p *pane) layout(render func(i int, state rowState) []string) {
	p.cache = map[int][]string{}
	p.starts = make([]int, len(p.rows))
	line := 0
	for i, r := range p.rows {
		p.starts[i] = line
		switch r.kind {
		case rowLine, rowFile, rowHunk, rowCommit:
			line++
		default:
			out := render(i, rowState{})
			p.cache[i] = out
			line += len(out)
		}
	}
	p.total = line
	p.clamp()
}

func (p *pane) rowHeight(i int) int {
	if i+1 < len(p.starts) {
		return p.starts[i+1] - p.starts[i]
	}
	return p.total - p.starts[i]
}

// rowAt returns the row shown on display line y of the pane, or -1.
func (p *pane) rowAt(y int) int {
	line := p.offset + y
	if line < 0 || line >= p.total {
		return -1
	}
	return sort.Search(len(p.starts), func(i int) bool { return p.starts[i] > line }) - 1
}

func (p *pane) clamp() {
	if p.cursor >= len(p.rows) {
		p.cursor = len(p.rows) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	if len(p.rows) > 0 && !p.rows[p.cursor].selectable() {
		if next := p.nextSelectable(p.cursor, 1); next >= 0 {
			p.cursor = next
		} else if prev := p.nextSelectable(p.cursor, -1); prev >= 0 {
			p.cursor = prev
		}
	}
	maxOffset := max(0, p.total-p.height)
	p.offset = min(max(p.offset, 0), maxOffset)
}

func (p *pane) nextSelectable(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(p.rows); i += dir {
		if p.rows[i].selectable() {
			return i
		}
	}
	return -1
}

// move moves the cursor n selectable rows, scrolling instead when there is nothing to select.
func (p *pane) move(n int) {
	dir := 1
	if n < 0 {
		dir, n = -1, -n
	}
	moved := false
	for range n {
		next := p.nextSelectable(p.cursor, dir)
		if next < 0 {
			break
		}
		p.cursor = next
		moved = true
	}
	if !moved {
		p.scroll(dir * n)
		return
	}
	p.ensureVisible()
}

// moveLines moves the cursor by about n display lines, for paging.
func (p *pane) moveLines(n int) {
	if len(p.rows) == 0 {
		return
	}
	target := p.starts[p.cursor] + n
	i := p.cursor
	for {
		next := p.nextSelectable(i, sign(n))
		if next < 0 || (n > 0 && p.starts[next] > target) || (n < 0 && p.starts[next] < target) {
			break
		}
		i = next
	}
	if i == p.cursor {
		if next := p.nextSelectable(i, sign(n)); next >= 0 {
			i = next
		}
	}
	p.cursor = i
	p.offset += n
	p.clamp()
	p.ensureVisible()
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	return 1
}

func (p *pane) top() {
	p.cursor, p.offset = 0, 0
	p.clamp()
}

func (p *pane) bottom() {
	p.cursor = len(p.rows) - 1
	p.offset = p.total
	p.clamp()
	if !p.rows[p.cursor].selectable() {
		if prev := p.nextSelectable(p.cursor, -1); prev >= 0 {
			p.cursor = prev
		}
	}
}

// scroll moves the viewport by n display lines, dragging the cursor along when it leaves the view.
func (p *pane) scroll(n int) {
	p.offset += n
	p.clamp()
	if len(p.rows) == 0 {
		return
	}
	start, end := p.starts[p.cursor], p.starts[p.cursor]+p.rowHeight(p.cursor)
	if end <= p.offset || start >= p.offset+p.height {
		if i := p.rowAt(0); n > 0 && i >= 0 {
			p.cursor = i
		} else if i := p.rowAt(p.height - 1); i >= 0 {
			p.cursor = i
		}
		if !p.rows[p.cursor].selectable() {
			if next := p.nextSelectable(p.cursor, sign(n)); next >= 0 {
				p.cursor = next
			}
		}
	}
}

func (p *pane) ensureVisible() {
	if len(p.rows) == 0 {
		return
	}
	start := p.starts[p.cursor]
	h := p.rowHeight(p.cursor)
	so := p.margin()
	switch {
	case h+2*so > p.height:
		// Too tall to keep a margin below: show its top.
		if start-so < p.offset || start >= p.offset+p.height {
			p.offset = start - so
		}
	case start-so < p.offset:
		p.offset = start - so
	case start+h+so > p.offset+p.height:
		p.offset = start + h + so - p.height
	}
	p.clamp()
}

// jump puts the cursor on row i and scrolls it near the top, scrolloff lines down.
func (p *pane) jump(i int) {
	if i < 0 || i >= len(p.rows) {
		return
	}
	p.cursor = i
	p.offset = p.starts[i] - p.margin()
	p.clamp()
	p.ensureVisible()
}

// selection returns the selected row range, inclusive, or the cursor alone.
func (p *pane) selection() (int, int) {
	if p.anchor < 0 {
		return p.cursor, p.cursor
	}
	return min(p.anchor, p.cursor), max(p.anchor, p.cursor)
}

// visible renders the rows in view.
func (p *pane) visible(render func(i int, state rowState) []string) []string {
	out := make([]string, 0, p.height)
	if len(p.rows) == 0 {
		return out
	}
	selStart, selEnd := p.selection()
	first := p.rowAt(0)
	if first < 0 {
		return out
	}
	var cursorHunk *git.Hunk
	if p.cursor < len(p.rows) {
		cursorHunk = p.rows[p.cursor].hunk
	}
	for i := first; i < len(p.rows) && len(out) < p.height; i++ {
		state := rowState{cursor: i == p.cursor, selected: p.anchor >= 0 && i >= selStart && i <= selEnd, side: p.side}
		state.activeHunk = cursorHunk != nil && p.rows[i].hunk == cursorHunk
		var lines []string
		if state.cursor || state.selected || state.activeHunk {
			lines = render(i, state)
		} else if cached, ok := p.cache[i]; ok {
			lines = cached
		} else {
			lines = render(i, state)
			p.cache[i] = lines
		}
		skip := 0
		if i == first {
			skip = p.offset - p.starts[i]
		}
		for _, l := range lines[min(skip, len(lines)):] {
			if len(out) == p.height {
				break
			}
			out = append(out, l)
		}
	}
	return out
}
