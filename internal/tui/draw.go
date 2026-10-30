package tui

import (
	"image/color"
	"strings"

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

// drawCut is draw, but marks a cut with "...".
func (t *theme) drawCut(s surface, width int, spans ...span) string {
	out := t.render(s, spans...)
	if ansi.StringWidth(out) > width {
		out = ansi.Truncate(out, width, t.style(elText, 0, s).Render("..."))
	}
	return t.draw(s, width, styledSpan(func(color.Color) string { return out }))
}
