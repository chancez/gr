package look

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/termtext"
)

// Span is a piece of a line: a value drawn as its element, with props. Only this package's
// constructors make one.
type Span struct {
	el    element
	text  string
	props Prop
	// styled, when set, draws text a foreign renderer styles, such as syntax highlighting, on the
	// surface's background.
	styled func(bg color.Color) string
}

func sp(el element, text string) Span {
	return Span{el: el, text: text}
}

// With adds props to a span.
func (s Span) With(p ...Prop) Span {
	for _, o := range p {
		s.props |= o
	}
	return s
}

// Styled is a span a foreign renderer draws, given the surface's background.
func Styled(draw func(bg color.Color) string) Span {
	return Span{styled: draw}
}

// DimIf dims spans when on, for what is finished.
func DimIf(on bool, spans ...Span) []Span {
	if !on {
		return spans
	}
	out := make([]Span, len(spans))
	for i, x := range spans {
		out[i] = x.With(Dim)
	}
	return out
}

// TextOf is the text of spans, without styles.
func TextOf(spans ...Span) string {
	var b strings.Builder
	for _, x := range spans {
		b.WriteString(x.text)
	}
	return b.String()
}

// IsDark reports whether the theme is for a dark background.
func (t *Theme) IsDark() bool {
	return t.isDark
}

// Background is a surface's background, for foreign renderers.
func (t *Theme) Background(s Surface) color.Color {
	return t.surfaces[s].bg
}

// Render draws spans on surface s as they are. Every span's text is made safe for the terminal.
func (t *Theme) Render(s Surface, spans ...Span) string {
	var b strings.Builder
	for _, x := range spans {
		if x.styled != nil {
			b.WriteString(x.styled(t.surfaces[s].bg))
			continue
		}
		if x.text == "" {
			continue
		}
		b.WriteString(t.style(x.el, x.props, s).Render(termtext.Line(x.text)))
	}
	return b.String()
}

// Draw draws spans on surface s exactly width cells wide: cut, or padded with the surface's
// background.
func (t *Theme) Draw(s Surface, width int, spans ...Span) string {
	if width <= 0 {
		return ""
	}
	out := ansi.Truncate(t.Render(s, spans...), width, "")
	if pad := width - ansi.StringWidth(out); pad > 0 {
		out += t.style(elText, 0, s).Render(strings.Repeat(" ", pad))
	}
	return out
}

// DrawCut is Draw, but marks a cut with "..." in the last span's style.
func (t *Theme) DrawCut(s Surface, width int, spans ...Span) string {
	out := t.Render(s, spans...)
	if ansi.StringWidth(out) > width {
		tail := sp(elText, "...")
		if n := len(spans); n > 0 && spans[n-1].styled == nil {
			tail.el, tail.props = spans[n-1].el, spans[n-1].props
		}
		out = ansi.Truncate(out, width, t.Render(s, tail))
	}
	return t.Draw(s, width, Styled(func(color.Color) string { return out }))
}

// Box is a bordered box width cells wide, its border in the accent when active. The border is
// layout; only its color comes from the theme.
func (t *Theme) Box(width int, active bool) lipgloss.Style {
	border := t.elements[elBorder]
	if active {
		border = t.elements[elBorderActive]
	}
	return lipgloss.NewStyle().Border(lipgloss.ASCIIBorder()).BorderForeground(border.GetForeground()).Padding(0, 1).Width(width)
}

// StyleHelp colors a key help view's keys, descriptions, and separators.
func (t *Theme) StyleHelp(h *help.Model) {
	key, desc, sep := t.style(elKey, 0, Plain), t.style(elKeyDesc, 0, Plain), t.style(elMuted, 0, Plain)
	h.Styles.ShortKey, h.Styles.FullKey = key, key
	h.Styles.ShortDesc, h.Styles.FullDesc = desc, desc
	h.Styles.ShortSeparator, h.Styles.FullSeparator = sep, sep
}

// EditorStyles are a text area's styles in the theme: text, and muted placeholder and prompt.
func (t *Theme) EditorStyles() textarea.Styles {
	styles := textarea.DefaultStyles(t.isDark)
	text, muted := t.style(elText, 0, Plain), t.style(elMuted, 0, Plain)
	for _, state := range []*textarea.StyleState{&styles.Focused, &styles.Blurred} {
		state.Text = text
		state.Placeholder = muted
		state.CursorLine = text
		state.EndOfBuffer = muted
		state.Prompt = muted
	}
	return styles
}

// Fit cuts or pads s, already styled, to exactly width cells. The padding is unstyled.
func Fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "")
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

// Wrap wraps text to width, for when Markdown cannot be rendered.
func Wrap(text string, width int) []string {
	return strings.Split(lipgloss.NewStyle().Width(width).Render(text), "\n")
}
