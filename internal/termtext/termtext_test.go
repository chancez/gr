package termtext

import "testing"

func TestLine(t *testing.T) {
	for in, want := range map[string]string{
		"plain text":               "plain text",
		"caf\u00e9":                "caf\u00e9",
		"\x1b[31mred\x1b[0m":       "^[[31mred^[[0m",
		"\x1b]0;title\x07":         "^[]0;title^G",
		"a\tb\nc\r":                "a^Ib^Jc^M",
		"del\x7f":                  "del^?",
		"csi\u009b31m":             `csi\u009b31m`,
		"bad\xff\xfe":              `bad\xff\xfe`,
		"replacement \ufffd stays": "replacement \ufffd stays",
	} {
		if got := Line(in); got != want {
			t.Errorf("Line(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBlock(t *testing.T) {
	in := "line one\n\tindented \x1b[2Jcleared\n"
	want := "line one\n\tindented ^[[2Jcleared\n"
	if got := Block(in); got != want {
		t.Errorf("Block(%q) = %q, want %q", in, got, want)
	}
}
