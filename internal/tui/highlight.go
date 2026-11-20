package tui

import (
	"image/color"
	"path"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"

	"github.com/chancez/gr/internal/git"
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

// lexer is the lexer for f, by its name, or for a file whose name says nothing, by its #! line.
func (h *highlighter) lexer(f git.FileDiff) chroma.Lexer {
	if l := h.cached(f.Path(), func() chroma.Lexer { return lexers.Match(f.Path()) }); l != nil {
		return l
	}
	name := interpreter(firstLine(f))
	if name == "" {
		return nil
	}
	return h.cached("#!"+name, func() chroma.Lexer { return interpreterLexer(name) })
}

func (h *highlighter) cached(key string, find func() chroma.Lexer) chroma.Lexer {
	if l, ok := h.lexers[key]; ok {
		return l
	}
	l := find()
	if l != nil {
		l = chroma.Coalesce(l)
	}
	h.lexers[key] = l
	return l
}

// firstLine is f's first line, from its hunks when they reach it.
func firstLine(f git.FileDiff) string {
	if len(f.Hunks) == 0 {
		return f.FirstLine
	}
	for _, l := range f.Hunks[0].Lines {
		if (f.NewPath == "" && l.OldLine == 1) || (f.NewPath != "" && l.NewLine == 1) {
			return l.Text
		}
	}
	return f.FirstLine
}

// interpreter is the program a #! line runs, without its version: "python" for
// "#!/usr/bin/env python3".
func interpreter(line string) string {
	rest, ok := strings.CutPrefix(line, "#!")
	if !ok {
		return ""
	}
	args := strings.Fields(rest)
	if len(args) == 0 {
		return ""
	}
	if path.Base(args[0]) == "env" {
		// env's own flags and variables come before the program, as in "env -S" or "env A=1".
		args = args[1:]
		for len(args) > 0 && (strings.HasPrefix(args[0], "-") || strings.Contains(args[0], "=")) {
			args = args[1:]
		}
		if len(args) == 0 {
			return ""
		}
	}
	return strings.TrimRight(path.Base(args[0]), "0123456789.")
}

// interpreterAliases name the lexers of interpreters chroma does not know by name.
var interpreterAliases = map[string]string{
	"dash":   "bash",
	"node":   "javascript",
	"nodejs": "javascript",
	"deno":   "typescript",
	"bun":    "javascript",
}

func interpreterLexer(name string) chroma.Lexer {
	if name == "" {
		return nil
	}
	if alias, ok := interpreterAliases[name]; ok {
		name = alias
	}
	return lexers.Get(name)
}

// line renders one line of f with syntax colors on bg. Lines are highlighted independently, so a
// construct spanning lines, like a block comment, may be colored only on its first line.
func (h *highlighter) line(f git.FileDiff, text string, bg color.Color) string {
	text = termtext.Line(text)
	base := lipgloss.NewStyle()
	if bg != nil {
		base = base.Background(bg)
	}
	l := h.lexer(f)
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
