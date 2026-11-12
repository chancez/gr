package tui

import (
	"fmt"

	"strings"
	"unicode"

	"github.com/chancez/gr/internal/tui/look"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// matcher matches rows with vim's smartcase: case-insensitive unless the pattern has an uppercase
// letter.
func matcher(pattern string) func(string) bool {
	if strings.IndexFunc(pattern, unicode.IsUpper) >= 0 {
		return func(s string) bool { return strings.Contains(s, pattern) }
	}
	lower := strings.ToLower(pattern)
	return func(s string) bool { return strings.Contains(strings.ToLower(s), lower) }
}

func (m *model) openSearch() tea.Cmd {
	in := textinput.New()
	in.Prompt = "/"
	in.SetWidth(max(m.width-2, 10))
	m.searchInput = in
	m.mode = modeSearch
	m.setFocus(focusMain)
	return m.searchInput.Focus()
}

func (m *model) searchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case pressed(m, msg, actCancel, theApp.Target(appSubject{})):
		m.mode = modeNormal
		return m, nil
	case msg.String() == "enter":
		m.mode = modeNormal
		if pattern := m.searchInput.Value(); pattern != "" {
			m.search = pattern
			m.searchPos, m.searchHits = 0, 0
		}
		m.searchStep(1)
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

// searchText searches for the text selected with the mouse, and lets it go, as moving on from it
// does.
func (m *model) searchText() {
	text, _ := m.selectedText()
	m.text = nil
	m.main.anchor = -1
	m.search = text
	m.searchPos, m.searchHits = 0, 0
	m.searchStep(1)
}

// searchStep moves to the next match in direction dir, wrapping at the ends of the view.
func (m *model) searchStep(dir int) {
	if m.search == "" {
		m.setError(fmt.Errorf("no search; / to search"))
		return
	}
	p := &m.main
	files := m.views[m.current].files
	match := matcher(m.search)
	var hits []int
	for i, r := range p.rows {
		if r.selectable() && match(r.searchText(files)) {
			hits = append(hits, i)
		}
	}
	if len(hits) == 0 {
		m.searchPos, m.searchHits = 0, 0
		m.setError(fmt.Errorf("no match for %q", m.search))
		return
	}
	next, wrapped := -1, false
	for k, i := range hits {
		if dir > 0 && i > p.cursor {
			next = k
			break
		}
		if dir < 0 && i < p.cursor {
			next = k
		}
	}
	if next < 0 {
		wrapped = true
		next = 0
		if dir < 0 {
			next = len(hits) - 1
		}
	}
	m.jump(func() { p.jump(hits[next]) })
	m.syncNav()
	m.searchPos, m.searchHits = next+1, len(hits)
	if wrapped {
		m.setStatus("search wrapped")
	}
}

// matchSpans returns the cell ranges in plain where the search pattern occurs, with smartcase.
func matchSpans(plain, pattern string) [][2]int {
	if pattern == "" {
		return nil
	}
	haystack, needle := plain, pattern
	if strings.IndexFunc(pattern, unicode.IsUpper) < 0 {
		haystack, needle = strings.ToLower(plain), strings.ToLower(pattern)
	}
	var spans [][2]int
	for from := 0; from < len(haystack); {
		i := strings.Index(haystack[from:], needle)
		if i < 0 {
			break
		}
		start := from + i
		end := start + len(needle)
		spans = append(spans, [2]int{ansi.StringWidth(plain[:start]), ansi.StringWidth(plain[:end])})
		from = end
	}
	return spans
}

// highlightMatches marks the search pattern's matches in a rendered line, keeping its other styles
// and its zones.
func (m *model) highlightMatches(line string) string {
	return look.KeepMarks(line, func(line string) string {
		spans := matchSpans(ansi.Strip(line), m.search)
		if len(spans) == 0 {
			return line
		}
		var b strings.Builder
		at := 0
		for _, sp := range spans {
			b.WriteString(ansi.Cut(line, at, sp[0]))
			b.WriteString(m.r.th.Render(look.SearchMatch, look.Text(ansi.Strip(ansi.Cut(line, sp[0], sp[1])))))
			at = sp[1]
		}
		b.WriteString(ansi.Cut(line, at, ansi.StringWidth(line)))
		return b.String()
	})
}

// searchLabel shows the active search in the status bar.
func (m *model) searchLabel() string {
	if m.search == "" {
		return ""
	}
	label := "/" + m.search
	if m.searchHits > 0 {
		label += fmt.Sprintf(" %d/%d", m.searchPos, m.searchHits)
	}
	return label
}
