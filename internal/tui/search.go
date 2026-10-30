package tui

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
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
	case key.Matches(msg, m.keys.Cancel):
		m.mode = modeNormal
		return m, nil
	case msg.String() == "enter":
		m.mode = modeNormal
		if pattern := m.searchInput.Value(); pattern != "" {
			m.search = pattern
		}
		m.searchStep(1)
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
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
	p.jump(hits[next])
	m.syncNav()
	status := fmt.Sprintf("/%s  match %d of %d", m.search, next+1, len(hits))
	if wrapped {
		status += ", wrapped"
	}
	m.setStatus("%s", status)
}
