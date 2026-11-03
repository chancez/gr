package tui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/tui/look"
)

// picker is a popup list to choose from, narrowed as you type by a fuzzy filter. It is driven by
// keys or the mouse: the wheel scrolls it, a click picks an entry, and a click outside closes it.
type picker struct {
	title  string
	items  []pickerItem
	filter textinput.Model
	// shown are the indexes of the items the filter matches, best first, and pos the selected one.
	shown []int
	pos   int
	top   int
	// pick acts on the chosen item.
	pick func(pickerItem) tea.Cmd
	// bound marks a menu of actions bound to subjects, which new data makes stale.
	bound bool
	// anchored places the popup at a point, as a menu opens where it was clicked, rather than near
	// the top and centered.
	anchored bool
	anchorX  int
	anchorY  int
	// x, y, width, and height are where the popup was last drawn, for the mouse.
	x      int
	y      int
	width  int
	height int
}

// pickerItem is one entry: its value, and a note shown after it, such as "current". A heading
// names the entries after it and cannot be picked; it shows only while nothing is typed.
type pickerItem struct {
	value string
	// full is the entry with what it acts on, for when typing hides the headings, as "File main.go:
	// comment" under "File main.go". Empty is the value.
	full    string
	note    string
	heading bool
	// run is what picking the entry does, for menus of actions.
	run func(*model) tea.Cmd
}

// text is how the entry shows: in full while filtering, when the headings are hidden.
func (it pickerItem) text(filtering bool) string {
	if filtering && it.full != "" {
		return it.full
	}
	return it.value
}

func newPicker(title string, items []pickerItem, pick func(pickerItem) tea.Cmd) *picker {
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "type to filter"
	p := &picker{title: title, items: items, filter: in, pick: pick}
	p.refilter()
	return p
}

// fuzzyScore scores how well value matches pattern, lower being better, or reports no match. A
// substring scores by where it starts; otherwise the pattern's letters must appear in order, and
// the match scores by how spread out they are. Case is ignored unless the pattern has an
// uppercase letter.
func fuzzyScore(value, pattern string) (int, bool) {
	if pattern == "" {
		return 0, true
	}
	if strings.IndexFunc(pattern, unicode.IsUpper) < 0 {
		value = strings.ToLower(value)
	}
	if i := strings.Index(value, pattern); i >= 0 {
		return i, true
	}
	start, j := -1, 0
	want := []rune(pattern)
	for i, r := range value {
		if r != want[j] {
			continue
		}
		if start < 0 {
			start = i
		}
		if j++; j == len(want) {
			return 1000 + i - start, true
		}
	}
	return 0, false
}

func (p *picker) refilter() {
	pattern := p.filter.Value()
	if pattern == "" {
		p.shown = p.shown[:0]
		for i := range p.items {
			p.shown = append(p.shown, i)
		}
		// Start on the first entry, with any heading above it in view.
		p.pos, p.top = 0, 0
		for p.pos < len(p.shown)-1 && p.items[p.shown[p.pos]].heading {
			p.pos++
		}
		return
	}
	type scored struct {
		i     int
		score int
	}
	var matches []scored
	for i, it := range p.items {
		if it.heading {
			continue
		}
		if s, ok := fuzzyScore(it.text(true), pattern); ok {
			matches = append(matches, scored{i, s})
		}
	}
	slices.SortStableFunc(matches, func(a, b scored) int { return a.score - b.score })
	p.shown = p.shown[:0]
	for _, m := range matches {
		p.shown = append(p.shown, m.i)
	}
	p.pos, p.top = 0, 0
}

// rows is how many entries fit in a popup of the given height: the border, title, and filter take
// four lines, and the count one more.
func pickerRows(height int) int {
	return max(height-5, 1)
}

// move moves the selection n entries, past headings.
func (p *picker) move(n int) {
	if len(p.shown) == 0 {
		return
	}
	dir := 1
	if n < 0 {
		dir = -1
	}
	pos := min(max(p.pos+n, 0), len(p.shown)-1)
	for pos >= 0 && pos < len(p.shown) && p.items[p.shown[pos]].heading {
		pos += dir
	}
	if pos < 0 || pos >= len(p.shown) {
		// Only headings that way: look the other way.
		for pos = min(max(p.pos+n, 0), len(p.shown)-1); pos >= 0 && pos < len(p.shown) && p.items[p.shown[pos]].heading; pos -= dir {
		}
		if pos < 0 || pos >= len(p.shown) {
			return
		}
	}
	p.pos = pos
	rows := pickerRows(p.height)
	// Keep the heading of an entry in view with it.
	first := p.pos
	if first > 0 && p.items[p.shown[first-1]].heading {
		first--
	}
	if first < p.top {
		p.top = first
	}
	if p.pos >= p.top+rows {
		p.top = p.pos - rows + 1
	}
}

func (p *picker) chosen() (pickerItem, bool) {
	if p.pos >= len(p.shown) || p.items[p.shown[p.pos]].heading {
		return pickerItem{}, false
	}
	return p.items[p.shown[p.pos]], true
}

// layout places the popup in a screen of width by height, as wide as its longest entry allows:
// centered in the upper part of the screen and large, or, anchored, as a menu, with its top left
// corner just right of its point, growing down, shifted up or left only as far as it needs to fit.
func (p *picker) layout(width, height int) {
	w := ansi.StringWidth(p.title) + 4
	for _, it := range p.items {
		w = max(w, ansi.StringWidth(it.text(true))+ansi.StringWidth(it.note)+8)
	}
	p.width = min(max(w, 40), width-4, 100)
	p.height = min(len(p.items)+5, height-4)
	p.x, p.y = (width-p.width)/2, 2
	if !p.anchored {
		// A popup of its own, as the palette, takes a good part of the screen, so it reads at a glance.
		p.width = min(max(w, width*3/5), width-4)
		p.x, p.y = (width-p.width)/2, max(height/8, 1)
	}
	if p.anchored {
		p.x = min(max(p.anchorX+1, 0), max(width-p.width, 0))
		p.y = max(min(p.anchorY, height-p.height), 0)
	}
	p.filter.SetWidth(p.width - 8)
}

func (p *picker) render(th *look.Theme) []string {
	inner := p.width - 4
	lines := []string{th.Draw(look.Plain, inner, look.Heading(p.title)), look.Fit(p.filter.View(), inner)}
	rows := pickerRows(p.height)
	// Entries under headings are indented beneath them.
	grouped := slices.ContainsFunc(p.shown, func(i int) bool { return p.items[i].heading })
	for i := p.top; i < p.top+rows; i++ {
		if i >= len(p.shown) {
			lines = append(lines, look.Fit("", inner))
			continue
		}
		it := p.items[p.shown[i]]
		if it.heading {
			// A heading is a divider naming the entries under it, as the overview's are.
			label := "-- " + it.value + " "
			lines = append(lines, th.Draw(look.Plain, inner, look.Muted(label+strings.Repeat("-", max(inner-len(label), 0)))))
			continue
		}
		surf := look.Plain
		if i == p.pos {
			surf = look.Cursor
		}
		value := it.text(p.filter.Value() != "")
		if grouped {
			value = "  " + value
		}
		spans := []look.Span{look.Text(value)}
		if it.note != "" {
			spans = append(spans, look.Muted("  "+it.note))
		}
		lines = append(lines, th.DrawCut(surf, inner, spans...))
	}
	shown, all := 0, 0
	for i, it := range p.items {
		if !it.heading {
			all++
			if slices.Contains(p.shown, i) {
				shown++
			}
		}
	}
	count := fmt.Sprintf("%d of %d", shown, all)
	lines = append(lines, th.Draw(look.Plain, inner, look.Muted(count), look.Muted("  enter picks, esc closes")))
	return strings.Split(th.Box(p.width, true).Render(strings.Join(lines, "\n")), "\n")
}

// at is the entry at screen row y, or -1.
func (p *picker) at(x, y int) int {
	// The border and title, then the filter, come before the first entry.
	i := y - p.y - 3
	if x < p.x || x >= p.x+p.width || i < 0 || i >= pickerRows(p.height) || p.top+i >= len(p.shown) {
		return -1
	}
	return p.top + i
}

func (p *picker) contains(x, y int) bool {
	return x >= p.x && x < p.x+p.width && y >= p.y && y < p.y+p.height
}

// overlay draws popup lines over screen lines at column x and row y.
func overlay(screen, popup []string, x, y int) []string {
	for i, l := range popup {
		row := y + i
		if row < 0 || row >= len(screen) {
			continue
		}
		line := screen[row]
		w := ansi.StringWidth(l)
		screen[row] = ansi.Cut(line, 0, x) + l + ansi.Cut(line, x+w, ansi.StringWidth(line))
	}
	return screen
}

func (m *model) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	switch msg.String() {
	case "esc":
		m.closePicker()
		return m, nil
	case "enter":
		it, ok := p.chosen()
		m.closePicker()
		if !ok {
			return m, nil
		}
		return m, p.pick(it)
	case "up", "ctrl+p":
		p.move(-1)
		return m, nil
	case "down", "ctrl+n":
		p.move(1)
		return m, nil
	case "pgup":
		p.move(-pickerRows(p.height))
		return m, nil
	case "pgdown":
		p.move(pickerRows(p.height))
		return m, nil
	}
	before := p.filter.Value()
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(msg)
	if p.filter.Value() != before {
		p.refilter()
	}
	return m, cmd
}

func (m *model) pickerWheel(msg tea.MouseWheelMsg) {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.picker.move(-3)
	case tea.MouseWheelDown:
		m.picker.move(3)
	}
}

func (m *model) pickerClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	p := m.picker
	if !p.contains(msg.X, msg.Y) {
		m.closePicker()
		return m, nil
	}
	i := p.at(msg.X, msg.Y)
	if i < 0 {
		return m, nil
	}
	p.pos = i
	it, ok := p.chosen()
	if !ok {
		return m, nil
	}
	m.closePicker()
	return m, p.pick(it)
}

func (m *model) openPicker(p *picker) tea.Cmd {
	m.picker = p
	m.mode = modePicker
	return p.filter.Focus()
}

func (m *model) closePicker() {
	m.mode = modeNormal
}
