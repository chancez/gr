package tui

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"github.com/chancez/gr/internal/tui/interact"
	"github.com/chancez/gr/internal/tui/look"
	"github.com/charmbracelet/x/ansi"
)

// shortHelp is the status bar's hints: what the cursor's subject does where it is, by key, then
// submit, help, and quit, which are always shown.
func (m *model) shortHelp() []interact.Key[*model] {
	var out []interact.Key[*model]
	chain := m.cursorChain()
	if t, ok := m.cursorTarget(); ok {
		// The cursor's subject first, then the change it is part of.
		near := []interact.Target[*model]{t}
		if m.data != nil {
			near = append(near, theChange.Target(changeSubject{}))
		}
		var hinted []interact.Bound[*model]
		titles := map[string]bool{}
		// Bindings without a description are second keys of the one before, already hinted.
		for _, k := range interact.Keys(m, near...) {
			if k.Help().Desc == "" || slices.ContainsFunc(hinted, k.Action.Same) {
				continue
			}
			hinted = append(hinted, k.Action)
			// A hint shows the title of what the key does here, which can be more specific than
			// the binding's own description. Two actions of one title, as a line's comment and
			// its file's, tell themselves apart by their help: "c comment" and "C comment on file".
			title := k.Action.Title
			if titles[title] {
				title = k.Action.Help
			}
			titles[title] = true
			k.Binding = key.NewBinding(key.WithKeys(k.Keys()...), key.WithHelp(k.Help().Key, title))
			out = append(out, k)
		}
	}
	// What is always worth knowing comes last, where renderHints keeps it.
	for _, a := range []any{actSubmit, actShowHelp, actQuit} {
		for _, k := range interact.Keys(m, chain...) {
			if k.Action.Is(a) {
				out = append(out, k)
				break
			}
		}
	}
	return out
}

// helpSection is one titled group in the full help.
type helpSection struct {
	title    string
	here     bool
	bindings []key.Binding
}

// helpOrder is help's sections, in order: their titles, and the kinds and places whose keys each
// shows. A key shows in the first section that has it.
func helpOrder() []struct {
	title string
	from  []interact.Keyed
} {
	return []struct {
		title string
		from  []interact.Keyed
	}{
		{"Sidebar", []interact.Keyed{sidebarPaneKind, sidebarCommits, sidebarOverview, sidebarDiff, sidebarInterdiff,
			sidebarFileList}},
		{"Diff", []interact.Keyed{diffPaneKind, fileHeaderList, folds}},
		{"Line", []interact.Keyed{lineKind, messageLineKind, hunkKind, foldKind}},
		{"File", []interact.Keyed{fileKind}},
		{"Commit", []interact.Keyed{commitKind}},
		{"Thread", []interact.Keyed{diffThreads, diffThreadKind}},
		{"Overview", []interact.Keyed{overviewPaneKind, overviewKind, overviewThreads, overviewThreadKind, summaryKind,
			reviewBars, reviewKind}},
		{"Change", []interact.Keyed{changeKind}},
		{"Folds", []interact.Keyed{foldPrefixKind}},
		{"Everywhere", []interact.Keyed{appKind}},
		{"Submitting", []interact.Keyed{verdictKind}},
		{"Confirming", []interact.Keyed{confirmKind}},
		{"Writing a comment", []interact.Keyed{composeBoxKind}},
		{"Unsaved comment", []interact.Keyed{closingKind}},
	}
}

// helpSections are the keys bound on kinds and places, by section, the sections of where the
// cursor is first and marked. A binding with no description is a second key of the one before it,
// already in its help.
func (m *model) helpSections() []helpSection {
	t, ok := m.cursorTarget()
	near := []interact.Target[*model]{m.paneTarget(m.focus != focusMain)}
	if ok {
		near = append(near, t)
	}
	var first, rest []helpSection
	for _, hs := range helpOrder() {
		sec := helpSection{title: hs.title}
		for _, from := range hs.from {
			for _, t := range near {
				sec.here = sec.here || t.Involves(from)
			}
			for _, b := range from.Bindings() {
				if b.Help().Desc != "" {
					sec.bindings = append(sec.bindings, b)
				}
			}
		}
		if sec.here {
			first = append(first, sec)
		} else {
			rest = append(rest, sec)
		}
	}
	// Each key shows once, in the first section that has it.
	seen := map[key.Help]bool{}
	var out []helpSection
	for _, sec := range append(first, rest...) {
		var kept []key.Binding
		for _, b := range sec.bindings {
			if !seen[b.Help()] {
				seen[b.Help()] = true
				kept = append(kept, b)
			}
		}
		if len(kept) > 0 {
			sec.bindings = kept
			out = append(out, sec)
		}
	}
	return out
}

// renderHelp lays the help sections out in columns, the current context's sections first and
// marked.
func (m *model) renderHelp() string {
	th := m.r.th
	sections := m.helpSections()

	var blocks [][]string
	for _, s := range sections {
		title := s.title
		if s.here {
			title += " (here)"
		}
		keyWidth := 0
		for _, b := range s.bindings {
			keyWidth = max(keyWidth, ansi.StringWidth(b.Help().Key))
		}
		lines := []string{th.Render(look.Plain, look.Heading(title))}
		for _, b := range s.bindings {
			h := b.Help()
			lines = append(lines, th.Render(look.Plain, look.Key(h.Key),
				look.Default(strings.Repeat(" ", keyWidth-ansi.StringWidth(h.Key)+2)), look.KeyDesc(h.Desc)))
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
			line.WriteString(look.Fit(cell, widths[i]+4))
		}
		b.WriteString(ansi.Truncate(line.String(), m.width, ""))
		b.WriteString("\n")
	}
	b.WriteString(th.Render(look.Plain, look.Muted("Any key closes this help.")))
	return b.String()
}

// renderHints draws as many hints as fit in width, always keeping the last keep. A click on a hint
// runs what its key does.
func (m *model) renderHints(keys []interact.Key[*model], width, keep int) string {
	th := m.r.th
	sep := ansi.StringWidth(" | ")
	hint := func(k interact.Key[*model]) []look.Span { return look.ClickableHint(k.Help().Key, k.Help().Desc) }
	n := len(keys)
	used := 0
	for _, k := range keys[max(n-keep, 0):] {
		used += ansi.StringWidth(look.TextOf(hint(k)...)) + sep
	}
	shown := keys[max(n-keep, 0):]
	var head []interact.Key[*model]
	for _, k := range keys[:max(n-keep, 0)] {
		w := ansi.StringWidth(look.TextOf(hint(k)...))
		if used+w+sep > width {
			break
		}
		head = append(head, k)
		used += w + sep
	}
	var spans []look.Span
	for i, k := range append(head, shown...) {
		if i > 0 {
			spans = append(spans, look.Muted(" | "))
		}
		spans = append(spans, m.mark("hints", modeNormal, k.Action, hint(k)...)...)
	}
	return th.Render(look.Plain, spans...)
}
