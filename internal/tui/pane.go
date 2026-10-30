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
	// reader moves between items, the comments of the overview's threads, keeping the selected one
	// at a reading position and scrolling through items taller than the pane. The overview uses it.
	reader bool
	// items are reader mode's stops, item the selected one.
	items []item
	item  int
	// segments returns the line offsets where a row's items start, after the row is rendered, or
	// nil for a row that is one item.
	segments func(i int) []int
	// marker is drawn in the left margin beside the selected item in reader mode.
	marker string
	// overscroll lets the view sit past the end of the content, showing blank lines, so content
	// that shrinks near the end does not drag the cursor down the screen. It drains as the view
	// scrolls back up.
	overscroll int
}

// item is one stop in reader mode: lines start to end, exclusive, of a row.
type item struct {
	row   int
	start int
	end   int
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
		case rowLine, rowFile, rowHunk, rowCommit, rowGap:
			line++
		default:
			out := render(i, rowState{})
			p.cache[i] = out
			line += len(out)
		}
	}
	p.total = line
	p.buildItems()
	p.clamp()
}

func (p *pane) buildItems() {
	p.items = nil
	if !p.reader {
		return
	}
	for i, r := range p.rows {
		if !r.selectable() {
			continue
		}
		end := p.starts[i] + p.rowHeight(i)
		var segs []int
		if p.segments != nil {
			segs = p.segments(i)
		}
		if len(segs) == 0 {
			segs = []int{0}
		}
		for k, s := range segs {
			it := item{row: i, start: p.starts[i] + s, end: end}
			if k+1 < len(segs) {
				it.end = p.starts[i] + segs[k+1]
			}
			p.items = append(p.items, it)
		}
	}
	p.item = min(max(p.item, 0), max(len(p.items)-1, 0))
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
	if p.offset <= maxOffset {
		// Back within the content: the overscroll has drained.
		p.overscroll = 0
	}
	p.offset = min(max(p.offset, 0), maxOffset+p.overscroll)
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

// readerNext reads on: through the selected item if its end is below the pane, else to the next
// item, placed at the reading position.
func (p *pane) readerNext() {
	if len(p.items) == 0 {
		return
	}
	cur := p.items[p.item]
	if cur.end > p.offset+p.height {
		// Scroll at most to where the item's end shows.
		p.offset = min(p.offset+max(p.height/2, 1), cur.end-p.height)
		p.clamp()
		return
	}
	if p.item+1 < len(p.items) {
		p.item++
		p.placeItem()
	}
}

// readerPrev reads back: up through the selected item if its start is above the pane, else to the
// previous item.
func (p *pane) readerPrev() {
	if len(p.items) == 0 {
		return
	}
	cur := p.items[p.item]
	if cur.start < p.offset {
		p.offset = max(p.offset-max(p.height/2, 1), cur.start-p.margin())
		p.clamp()
		return
	}
	if p.item > 0 {
		p.item--
		p.placeItem()
	}
}

// placeItem scrolls the selected item to the reading position, scrolloff lines from the top.
func (p *pane) placeItem() {
	it := p.items[p.item]
	p.cursor = it.row
	p.offset = it.start - p.margin()
	p.clamp()
}

// readerSelect selects item i and places it.
func (p *pane) readerSelect(i int) {
	if i < 0 || i >= len(p.items) {
		return
	}
	p.item = i
	p.placeItem()
}

// readerScroll scrolls n lines and selects the item at the reading position.
func (p *pane) readerScroll(n int) {
	p.offset += n
	p.clamp()
	p.selectAt(p.offset + p.margin())
}

// selectAt selects the item covering line, else the first one starting below it, else the last.
func (p *pane) selectAt(line int) {
	if len(p.items) == 0 {
		return
	}
	k := sort.Search(len(p.items), func(k int) bool { return p.items[k].end > line })
	p.item = min(k, len(p.items)-1)
	p.cursor = p.items[p.item].row
}

// itemSegment is the selected item's index within its row, for restoring it after a rebuild.
func (p *pane) itemSegment() int {
	if !p.reader || p.item >= len(p.items) {
		return 0
	}
	seg := 0
	for k := p.item - 1; k >= 0 && p.items[k].row == p.items[p.item].row; k-- {
		seg++
	}
	return seg
}

// syncItem selects row i's item seg, after the cursor moved by row.
func (p *pane) syncItem(i, seg int) {
	if !p.reader {
		return
	}
	for k, it := range p.items {
		if it.row == i {
			p.item = k
			for seg > 0 && p.item+1 < len(p.items) && p.items[p.item+1].row == i {
				p.item++
				seg--
			}
			return
		}
	}
}

// cursorY is the screen line the cursor row, or in reader mode the selected item, starts on.
func (p *pane) cursorY() int {
	if p.reader && p.item < len(p.items) {
		return p.items[p.item].start - p.offset
	}
	if p.cursor >= len(p.starts) {
		return 0
	}
	return p.starts[p.cursor] - p.offset
}

// placeAt puts the cursor on row i, scrolled so the row starts on screen line y. Rebuilds use it
// to keep the cursor still while rows around it change height.
func (p *pane) placeAt(i, seg, y int) {
	p.cursor = i
	p.syncItem(i, seg)
	start := p.starts[i]
	if p.reader && p.item < len(p.items) {
		start = p.items[p.item].start
	}
	want := start - y
	p.overscroll = max(want-max(0, p.total-p.height), 0)
	p.offset = want
	p.clamp()
	if !p.reader {
		p.ensureVisible()
	}
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
	p.syncItem(i, 0)
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
		for k, l := range lines[min(skip, len(lines)):] {
			if len(out) == p.height {
				break
			}
			if p.reader {
				// The left margin marks the selected item.
				marker := " "
				if line := p.starts[i] + skip + k; p.item < len(p.items) && line >= p.items[p.item].start && line < p.items[p.item].end {
					marker = p.marker
				}
				l = marker + l
			}
			out = append(out, l)
		}
	}
	return out
}
