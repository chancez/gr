package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"

	"github.com/chancez/gr/internal/termtext"
)

// highlighter colors source lines with chroma, leaving the background to the caller so diff colors
// show through.
type highlighter struct {
	style   *chroma.Style
	lexers  map[string]chroma.Lexer
	enabled bool
}

func newHighlighter(isDark bool) *highlighter {
	name := "github"
	if isDark {
		name = "github-dark"
	}
	return &highlighter{style: chromastyles.Get(name), lexers: map[string]chroma.Lexer{}, enabled: true}
}

func (h *highlighter) lexer(path string) chroma.Lexer {
	if l, ok := h.lexers[path]; ok {
		return l
	}
	l := lexers.Match(path)
	if l != nil {
		l = chroma.Coalesce(l)
	}
	h.lexers[path] = l
	return l
}

// line renders one line of path with syntax colors on bg. Lines are highlighted independently, so a
// construct spanning lines, like a block comment, may be colored only on its first line.
func (h *highlighter) line(path, text string, bg color.Color) string {
	text = termtext.Line(text)
	base := lipgloss.NewStyle()
	if bg != nil {
		base = base.Background(bg)
	}
	l := h.lexer(path)
	if !h.enabled || l == nil {
		return base.Render(text)
	}
	it, err := l.Tokenise(nil, text)
	if err != nil {
		return base.Render(text)
	}
	var b strings.Builder
	for tok := it(); tok != chroma.EOF; tok = it() {
		value := strings.TrimRight(tok.Value, "\n")
		if value == "" {
			continue
		}
		s := base
		entry := h.style.Get(tok.Type)
		if entry.Colour.IsSet() {
			s = s.Foreground(lipgloss.Color(entry.Colour.String()))
		}
		if entry.Bold == chroma.Yes {
			s = s.Bold(true)
		}
		if entry.Italic == chroma.Yes {
			s = s.Italic(true)
		}
		b.WriteString(s.Render(value))
	}
	return b.String()
}
