package tui

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/guygrigsby/vimbubble/v2"

	"github.com/chancez/gr/internal/tui/look"
)

// prompt is the status line's input, for a search or the file filter: one line of a text area,
// edited with vim's modes when vim is on, as the comment box is.
type prompt struct {
	area textarea.Model
	vim  *vimbubble.Modal
}

// openPrompt opens the prompt, labeled label, holding value, in mode md.
func (m *model) openPrompt(md mode, label, placeholder, value string) tea.Cmd {
	a := textarea.New()
	a.Prompt = label
	a.Placeholder = placeholder
	a.ShowLineNumbers = false
	a.SetStyles(m.r.th.EditorStyles())
	// Enter is the prompt's, to run it: the text is one line.
	a.KeyMap.InsertNewline.SetEnabled(false)
	width := m.width
	if m.vim {
		// Room for vim's mode at the right, as "-- NORMAL --".
		width -= 14
	}
	a.SetWidth(max(width, 10))
	a.SetHeight(1)
	a.SetValue(value)
	m.prompt = prompt{area: a}
	if m.vim {
		// It edits the prompt's own text area, so it is made once the prompt is in place.
		m.prompt.vim = vimbubble.New(&m.prompt.area)
		m.prompt.vim.SetEnabled(true)
		m.prompt.vim.SetMode(vimbubble.Insert)
	}
	m.mode = md
	return m.prompt.area.Focus()
}

// promptKey edits the prompt with msg: enter closes it and runs done on what was typed, and esc
// closes it, after leaving vim's insert or visual mode, as in the comment box.
func (m *model) promptKey(msg tea.KeyPressMsg, done func(string)) (tea.Model, tea.Cmd) {
	msg = unshifted(msg)
	vim := m.prompt.vim
	switch {
	case vim != nil && msg.Key().Code == tea.KeyEscape && vim.Mode() != vimbubble.Normal:
		_, c := vim.Update(msg)
		return m, c
	case pressed(m, msg, actCancel, theApp.Target(appSubject{})):
		m.mode = modeNormal
		return m, nil
	case msg.String() == "enter":
		m.mode = modeNormal
		done(m.prompt.area.Value())
		return m, nil
	}
	if vim != nil {
		if consumed, c := vim.Update(msg); consumed {
			return m, c
		}
	}
	var cmd tea.Cmd
	m.prompt.area, cmd = m.prompt.area.Update(msg)
	return m, cmd
}

// renderPrompt draws the prompt at width, with vim's mode at the right.
func (m *model) renderPrompt(width int) string {
	if m.prompt.vim == nil {
		return look.Fit(m.prompt.area.View(), width)
	}
	mode := m.r.th.Render(look.Plain, look.Mode(" "+m.prompt.vim.ModeLabel()+" "))
	return look.Fit(m.prompt.area.View(), max(width-ansi.StringWidth(mode), 0)) + mode
}

// placePromptCursor puts the prompt's cursor at column x of the status line.
func (m *model) placePromptCursor(x int) {
	m.prompt.area.SetCursorColumn(max(x-ansi.StringWidth(m.prompt.area.Prompt), 0))
}
