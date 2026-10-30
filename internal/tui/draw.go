package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/chancez/gr/internal/termtext"
	"github.com/charmbracelet/x/ansi"
)

// span is a piece of a line: text of one element, with props.
type span struct {
	el    element
	text  string
	props props
	// styled, when set, draws text a foreign renderer styles, such as syntax highlighting, on the
	// surface's background.
	styled func(bg color.Color) string
}

// sp is a span of text as el.
func sp(el element, text string, p ...props) span {
	s := span{el: el, text: text}
	for _, o := range p {
		s.props |= o
	}
	return s
}

// styledSpan is a span a foreign renderer draws.
func styledSpan(draw func(bg color.Color) string) span {
	return span{styled: draw}
}

// render draws spans on surface s as they are. Every span's text is made safe for the terminal.
func (t *theme) render(s surface, spans ...span) string {
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

// draw draws spans on surface s exactly width cells wide: cut, or padded with the surface's
// background.
func (t *theme) draw(s surface, width int, spans ...span) string {
	if width <= 0 {
		return ""
	}
	out := ansi.Truncate(t.render(s, spans...), width, "")
	if pad := width - ansi.StringWidth(out); pad > 0 {
		out += t.style(elText, 0, s).Render(strings.Repeat(" ", pad))
	}
	return out
}

// drawCut is draw, but marks a cut with "..." in the last span's style.
func (t *theme) drawCut(s surface, width int, spans ...span) string {
	out := t.render(s, spans...)
	if ansi.StringWidth(out) > width {
		tail := sp(elText, "...")
		if n := len(spans); n > 0 && spans[n-1].styled == nil {
			tail.el, tail.props = spans[n-1].el, spans[n-1].props
		}
		out = ansi.Truncate(out, width, t.render(s, tail))
	}
	return t.draw(s, width, styledSpan(func(color.Color) string { return out }))
}

// box is a bordered box width cells wide, its border in the accent when active. The border is
// layout; only its color comes from the theme.
func (t *theme) box(width int, active bool) lipgloss.Style {
	border := t.elements[elBorder]
	if active {
		border = t.elements[elBorderActive]
	}
	return lipgloss.NewStyle().Border(lipgloss.ASCIIBorder()).BorderForeground(border.GetForeground()).Padding(0, 1).Width(width)
}

// spanText is the text of spans, without styles.
func spanText(spans []span) string {
	var b strings.Builder
	for _, x := range spans {
		b.WriteString(x.text)
	}
	return b.String()
}

// fit cuts or pads s, already styled, to exactly width cells. The padding is unstyled.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "")
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

// wrap wraps text to width, for when Markdown cannot be rendered.
func wrap(text string, width int) []string {
	return strings.Split(lipgloss.NewStyle().Width(width).Render(text), "\n")
}
