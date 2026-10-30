// Package termtext makes text from repositories and reviews safe to write to a terminal.
//
// File contents, commit messages, names, and comments can hold control characters, and a terminal
// acts on them: an escape sequence can recolor the screen, move the cursor, or set the window
// title. They are shown instead in caret notation, as cat -v does: ESC as ^[, BEL as ^G, DEL as ^?.
// C1 controls and bytes that are not UTF-8 are shown as escapes, such as \u009b and \xff.
package termtext

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Line makes s safe as one line: every control character is shown, newlines and tabs included.
func Line(s string) string {
	return clean(s, false)
}

// Block makes s safe as text of several lines: newlines and tabs are kept, and every other control
// character is shown.
func Block(s string) string {
	return clean(s, true)
}

func clean(s string, block bool) string {
	if safe(s, block) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && n == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case !control(r, block):
			b.WriteString(s[i : i+n])
		case r < 0x20:
			b.WriteByte('^')
			b.WriteByte(byte(r) + '@')
		case r == 0x7f:
			b.WriteString("^?")
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
		i += n
	}
	return b.String()
}

// safe reports whether s has nothing to show differently, the common case, so it is returned as is.
func safe(s string, block bool) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if control(r, block) {
			return false
		}
	}
	return true
}

// control reports whether a terminal could act on r: C0 controls, DEL, and C1 controls. In a block,
// newlines and tabs are text.
func control(r rune, block bool) bool {
	if block && (r == '\n' || r == '\t') {
		return false
	}
	return r < 0x20 || r >= 0x7f && r < 0xa0
}

// NewWriter returns a writer that shows control characters in what it writes to w, as Block
// does. Each write is cleaned on its own, so a rune split across writes shows as bytes; fmt and
// encoders write whole strings.
func NewWriter(w io.Writer) io.Writer {
	return writer{w}
}

type writer struct {
	w io.Writer
}

func (w writer) Write(p []byte) (int, error) {
	s := string(p)
	if clean := Block(s); clean != s {
		if _, err := io.WriteString(w.w, clean); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return w.w.Write(p)
}
