// Package cells reads a terminal screen into cells, for tests that compare how screens look.
package cells

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Cell is one character on screen and how it is drawn.
type Cell struct {
	R     rune
	Style Style
}

// Style is the drawing state that SGR escapes set. Colors keep their parameters, such as "38;2;1;2;3".
type Style struct {
	fg        string
	bg        string
	bold      bool
	faint     bool
	italic    bool
	underline bool
	reverse   bool
	strike    bool
}

// Parse reads a screen into lines of cells, so two screens compare by how they look rather than by
// the escapes that draw them: a style can be set in one escape or in several.
func Parse(screen string) [][]Cell {
	var out [][]Cell
	for line := range strings.SplitSeq(screen, "\n") {
		var st Style
		var row []Cell
		for i := 0; i < len(line); {
			if rest, ok := strings.CutPrefix(line[i:], "\x1b["); ok {
				end := strings.IndexByte(rest, 'm')
				st.apply(strings.Split(rest[:end], ";"))
				i += 2 + end + 1
				continue
			}
			r, n := utf8.DecodeRuneInString(line[i:])
			c := Cell{R: r, Style: st}
			if r == ' ' {
				// A space shows only its background and lines through it, not its text's color.
				c.Style = Style{bg: st.bg, underline: st.underline, reverse: st.reverse, strike: st.strike}
			}
			row = append(row, c)
			i += n
		}
		out = append(out, row)
	}
	return out
}

func (s *Style) apply(params []string) {
	for i := 0; i < len(params); i++ {
		switch p := params[i]; p {
		case "", "0":
			*s = Style{}
		case "1":
			s.bold = true
		case "2":
			s.faint = true
		case "3":
			s.italic = true
		case "4":
			s.underline = true
		case "7":
			s.reverse = true
		case "9":
			s.strike = true
		case "22":
			s.bold, s.faint = false, false
		case "23":
			s.italic = false
		case "24":
			s.underline = false
		case "27":
			s.reverse = false
		case "29":
			s.strike = false
		case "39":
			s.fg = ""
		case "49":
			s.bg = ""
		case "38", "48":
			n := 3
			if i+1 < len(params) && params[i+1] == "2" {
				n = 5
			}
			color := strings.Join(params[i:min(i+n, len(params))], ";")
			if p == "38" {
				s.fg = color
			} else {
				s.bg = color
			}
			i += n - 1
		default:
			if n, err := strconv.Atoi(p); err == nil && (n >= 30 && n <= 37 || n >= 90 && n <= 97) {
				s.fg = p
			} else if err == nil && (n >= 40 && n <= 47 || n >= 100 && n <= 107) {
				s.bg = p
			} else {
				panic("unknown SGR parameter " + p)
			}
		}
	}
}
