package look

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Zones mark where spans are drawn, so a click can be traced back to what was drawn there. A
// marked span is wrapped in two APC sequences, which take no width and survive cutting and
// padding; Scan strips them from the final frame and records where each marked span landed.

const (
	zoneOpen  = "\x1b_grz:"
	zoneClose = "\x1b_grz/\x1b\\"
	apcEnd    = "\x1b\\"
)

func markZone(id, rendered string) string {
	return zoneOpen + id + apcEnd + rendered + zoneClose
}

// Zone is where a marked span was drawn: row Y, from column X, W cells wide.
type Zone struct {
	X int
	Y int
	W int
}

// Contains reports whether the cell at x, y is in the zone.
func (z Zone) Contains(x, y int) bool {
	return y == z.Y && x >= z.X && x < z.X+z.W
}

// Scan strips zone marks from a frame and returns where each zone was drawn, by id. A zone cut
// before its end ends with its line.
func Scan(frame string) (string, map[string][]Zone) {
	zones := map[string][]Zone{}
	lines := strings.Split(frame, "\n")
	for y, line := range lines {
		if !strings.Contains(line, zoneOpen) {
			continue
		}
		type open struct {
			id string
			x  int
		}
		var stack []open
		var b strings.Builder
		for line != "" {
			i := strings.Index(line, "\x1b_grz")
			if i < 0 {
				b.WriteString(line)
				break
			}
			b.WriteString(line[:i])
			line = line[i:]
			col := ansi.StringWidth(b.String())
			if strings.HasPrefix(line, zoneClose) {
				line = line[len(zoneClose):]
				if n := len(stack); n > 0 {
					o := stack[n-1]
					stack = stack[:n-1]
					zones[o.id] = append(zones[o.id], Zone{X: o.x, Y: y, W: col - o.x})
				}
				continue
			}
			end := strings.Index(line, apcEnd)
			if end < 0 {
				break
			}
			stack = append(stack, open{id: line[len(zoneOpen):end], x: ansi.StringWidth(b.String())})
			line = line[end+len(apcEnd):]
		}
		width := ansi.StringWidth(b.String())
		for _, o := range stack {
			zones[o.id] = append(zones[o.id], Zone{X: o.x, Y: y, W: width - o.x})
		}
		lines[y] = b.String()
	}
	return strings.Join(lines, "\n"), zones
}

// KeepMarks restyles line with f, which keeps its text and width, and puts its zone marks back at
// the cells they were at, so restyling part of a line, as a search match does, leaves what was
// marked in it clickable.
func KeepMarks(line string, f func(string) string) string {
	type mark struct {
		col int
		seq string
	}
	var marks []mark
	var clean strings.Builder
	for rest := line; rest != ""; {
		i := strings.Index(rest, "\x1b_grz")
		if i < 0 {
			clean.WriteString(rest)
			break
		}
		clean.WriteString(rest[:i])
		end := strings.Index(rest[i:], apcEnd)
		if end < 0 {
			clean.WriteString(rest[i:])
			break
		}
		marks = append(marks, mark{col: ansi.StringWidth(clean.String()), seq: rest[i : i+end+len(apcEnd)]})
		rest = rest[i+end+len(apcEnd):]
	}
	out := f(clean.String())
	if len(marks) == 0 {
		return out
	}
	// Each mark goes just before the cell it was at, after the styles that start there.
	var b strings.Builder
	var state byte
	col := 0
	for out != "" {
		seq, width, n, next := ansi.DecodeSequence(out, state, nil)
		for width > 0 && len(marks) > 0 && marks[0].col <= col {
			b.WriteString(marks[0].seq)
			marks = marks[1:]
		}
		b.WriteString(seq)
		col += width
		state = next
		out = out[n:]
	}
	for _, mk := range marks {
		b.WriteString(mk.seq)
	}
	return b.String()
}

// Mark marks spans as zone id, as a control is.
func Mark(id string, spans ...Span) []Span {
	out := make([]Span, len(spans))
	for i, x := range spans {
		x.zone = id
		out[i] = x
	}
	return out
}

// MarkSHA marks the shas among spans as zone id, as the commit a thread's location names.
func MarkSHA(id string, spans ...Span) []Span {
	out := make([]Span, len(spans))
	for i, x := range spans {
		if x.el == elSHA {
			x.zone = id
		}
		out[i] = x
	}
	return out
}
