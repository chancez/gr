package tui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/charmbracelet/x/ansi"
)

// helpContext is where the keys apply: the sidebar, the overview, or a diff.
type helpContext int

const (
	contextSidebar helpContext = iota
	contextOverview
	contextDiff
)

func (m *model) helpContext() helpContext {
	switch {
	case m.focus != focusMain:
		return contextSidebar
	case m.data != nil && m.views[m.current].kind == viewOverview:
		return contextOverview
	}
	return contextDiff
}

// hint is a binding shown in help only, for keys handled outside the keymap or described
// differently in one context.
func hint(keys, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys), key.WithHelp(keys, desc))
}

// shortHelp is the status bar's hints for the focused pane and the row under the cursor. The last
// two, help and quit, are always shown.
func (m *model) shortHelp() []key.Binding {
	k := m.keys
	tail := []key.Binding{k.Help, k.Quit}
	switch m.helpContext() {
	case contextSidebar:
		return append([]key.Binding{hint("j/k", "move"), hint("enter/l", "open"), k.Reviewed,
			hint("ctrl+j/k", "other list"), k.Focus}, tail...)
	case contextOverview:
		return append([]key.Binding{hint("j/k", "scroll"), hint("t/T", "threads"), hint("enter", "expand"),
			hint("o", "open in diff"), k.Reply, k.Resolve, hint("F", "filter")}, tail...)
	}
	if c := m.main.cursor; c < len(m.main.rows) {
		switch m.main.rows[c].kind {
		case rowThread:
			return append([]key.Binding{k.Reply, k.Resolve, k.Address, hint("enter", "expand"), k.Edit,
				hint("t/T", "threads")}, tail...)
		case rowGap:
			return append([]key.Binding{hint("enter", "expand"), hint("zR/zM", "all folds"),
				hint("J/K", "hunks")}, tail...)
		}
	}
	return append([]key.Binding{k.Comment, k.Select, k.Reviewed, hint("J/K", "hunks"), hint("]/[", "files"),
		k.Submit}, tail...)
}

// helpSection is one titled group in the full help.
type helpSection struct {
	title    string
	context  helpContext
	global   bool
	bindings []key.Binding
}

func (m *model) helpSections() []helpSection {
	k := m.keys
	return []helpSection{
		{title: "Everywhere", global: true, bindings: []key.Binding{
			hint("tab/shift+tab", "next/previous pane"), hint("h/l", "sidebar/diff"), k.AllChanges,
			hint("}/{", "next/prev commit"), k.Search, hint("n/N", "next/prev match"), k.Split, k.Reload,
			k.Submit, k.Discard, k.Help, k.Quit,
		}},
		{title: "Sidebar", context: contextSidebar, bindings: []key.Binding{
			hint("j/k", "move"), hint("d/u, f/b", "half page, page"), hint("g/G", "top, bottom"),
			hint("enter/l", "open"), hint("ctrl+j/k", "files/commits list"), k.Reviewed, k.CommitDone,
		}},
		{title: "Overview", context: contextOverview, bindings: []key.Binding{
			hint("j/k", "scroll"), hint("d/u, f/b", "half page, page"), hint("t/T", "next/prev thread"),
			hint("enter", "expand/collapse thread"), hint("o", "open thread in its diff"), k.Filter,
			hint("c", "comment on the change"),
		}},
		{title: "Diff", context: contextDiff, bindings: []key.Binding{
			hint("j/k", "move"), hint("d/u, f/b", "half page, page"), hint("]/[", "next/prev file"),
			hint("J/K", "next/prev hunk"), hint(")/(", "next/prev change"), hint("t/T", "next/prev thread"),
			hint("h/l", "old/new side in split"), k.Select, k.Comment, k.FileComment, k.Reviewed, k.CommitDone,
		}},
		{title: "Threads", global: true, bindings: []key.Binding{
			k.Reply, hint("x", "resolve/reopen"), k.Address, k.Edit, k.Delete,
		}},
		{title: "Folds", context: contextDiff, bindings: []key.Binding{
			hint("enter", "expand/collapse"), hint("zo/zc/za", "open/close/toggle"), hint("zR/zM", "open/close all"),
		}},
		{title: "Writing a comment", global: true, bindings: []key.Binding{k.Save, k.ExternalEdit, k.Cancel}},
	}
}

// renderHelp lays the help sections out in columns, the current context's sections first and
// marked.
func (m *model) renderHelp() string {
	st := m.r.st
	here := m.helpContext()
	var sections, rest []helpSection
	for _, s := range m.helpSections() {
		if !s.global && s.context == here {
			sections = append(sections, s)
		} else {
			rest = append(rest, s)
		}
	}
	sections = append(sections, rest...)

	var blocks [][]string
	for _, s := range sections {
		title := s.title
		if !s.global && s.context == here {
			title += " (here)"
		}
		keyWidth := 0
		for _, b := range s.bindings {
			keyWidth = max(keyWidth, ansi.StringWidth(b.Help().Key))
		}
		lines := []string{st.heading.Render(title)}
		for _, b := range s.bindings {
			h := b.Help()
			lines = append(lines, st.helpKey.Render(h.Key)+strings.Repeat(" ", keyWidth-ansi.StringWidth(h.Key)+2)+st.helpDesc.Render(h.Desc))
		}
		blocks = append(blocks, append(lines, ""))
	}

	// Fill columns top to bottom, starting a new column when the next block would not fit.
	height := max(m.height-2, 10)
	var columns [][]string
	var col []string
	for _, b := range blocks {
		if len(col) > 0 && len(col)+len(b) > height {
			columns = append(columns, col)
			col = nil
		}
		col = append(col, b...)
	}
	columns = append(columns, col)

	widths := make([]int, len(columns))
	rows := 0
	for i, c := range columns {
		for _, l := range c {
			widths[i] = max(widths[i], ansi.StringWidth(l))
		}
		rows = max(rows, len(c))
	}
	var b strings.Builder
	for y := range rows {
		var line strings.Builder
		for i, c := range columns {
			cell := ""
			if y < len(c) {
				cell = c[y]
			}
			line.WriteString(fit(cell, widths[i]+4, nil))
		}
		b.WriteString(ansi.Truncate(line.String(), m.width, ""))
		b.WriteString("\n")
	}
	b.WriteString(st.muted.Render("Any key closes this help."))
	return b.String()
}

// renderHints draws as many hints as fit in width, always keeping the last two.
func (m *model) renderHints(bindings []key.Binding, width int) string {
	st := m.r.st
	sep := st.muted.Render(" | ")
	item := func(b key.Binding) string {
		return st.helpKey.Render(b.Help().Key) + " " + st.helpDesc.Render(b.Help().Desc)
	}
	n := len(bindings)
	keep := bindings[max(n-2, 0):]
	var tail []string
	for _, b := range keep {
		tail = append(tail, item(b))
	}
	used := ansi.StringWidth(strings.Join(tail, sep))
	var head []string
	for _, b := range bindings[:max(n-2, 0)] {
		next := item(b)
		if used+ansi.StringWidth(next)+2*ansi.StringWidth(sep) > width {
			break
		}
		head = append(head, next)
		used += ansi.StringWidth(next) + ansi.StringWidth(sep)
	}
	return strings.Join(append(head, tail...), sep)
}
