package look

import (
	"os"
	"strings"
)

// Glyphs are the symbols a theme draws with: plain ASCII, Unicode symbols nearly every font has, or
// Nerd Font icons. A font's glyphs cannot be asked about, so Nerd Font icons are used only where the
// terminal is known to bring them.
type Glyphs int

// A theme draws in ASCII until told otherwise.
const (
	GlyphsASCII Glyphs = iota
	GlyphsUnicode
	GlyphsNerd
)

// ParseGlyphs is the glyphs named by s: "ascii", "unicode", or "nerd".
func ParseGlyphs(s string) (Glyphs, bool) {
	switch s {
	case "ascii":
		return GlyphsASCII, true
	case "unicode":
		return GlyphsUnicode, true
	case "nerd":
		return GlyphsNerd, true
	}
	return 0, false
}

// GlyphsFor is the glyphs a terminal can draw, from what it calls itself in answer to XTVERSION:
// Nerd Font icons in the terminals that bundle them, as kitty, WezTerm, and Ghostty do, and
// Unicode elsewhere.
func GlyphsFor(terminal string) Glyphs {
	t := strings.ToLower(terminal)
	for _, name := range []string{"kitty", "wezterm", "ghostty"} {
		if strings.Contains(t, name) {
			return GlyphsNerd
		}
	}
	return GlyphsUnicode
}

// GlyphsFromEnv is the glyphs the environment says the terminal can draw, before it answers
// XTVERSION or where it does not: ASCII on the Linux console, Nerd Font icons where a terminal that
// bundles them says so, and Unicode elsewhere.
func GlyphsFromEnv() Glyphs {
	switch {
	case os.Getenv("TERM") == "linux":
		return GlyphsASCII
	case os.Getenv("KITTY_WINDOW_ID") != "", os.Getenv("TERM") == "xterm-kitty", os.Getenv("GHOSTTY_RESOURCES_DIR") != "":
		return GlyphsNerd
	}
	return GlyphsFor(os.Getenv("TERM_PROGRAM"))
}

// Glyph is a symbol's role, drawn as the theme's glyphs have it. An empty glyph is not drawn, as a
// button's icon in ASCII.
type Glyph int

const (
	// GlyphNone draws nothing, as for a control without an icon.
	GlyphNone Glyph = iota
	GlyphOpen
	GlyphFolded
	GlyphChecked
	GlyphUnchecked
	GlyphComment
	GlyphMoreAbove
	GlyphMoreBelow
	GlyphReviewed
	GlyphDraft
	GlyphStateOpen
	GlyphStateAddressed
	GlyphStateResolved
	GlyphReply
	GlyphResolve
	GlyphReopen
	GlyphAddressed
	GlyphEdit
	GlyphDelete
	GlyphSave
	GlyphCancel
	GlyphEditor
	GlyphPrev
	GlyphNext
	GlyphFilter
	GlyphApprove
	GlyphRequestChanges
	GlyphVerdictComment
	GlyphYes
	GlyphNo
	numGlyphs
)

// glyphSets are each glyphs' symbols, by role. Nerd Font icons are Font Awesome's, at the code
// points Nerd Fonts keeps them at.
var glyphSets = map[Glyphs][numGlyphs]string{
	GlyphsASCII: {
		GlyphOpen:      "v",
		GlyphFolded:    ">",
		GlyphChecked:   "[x]",
		GlyphUnchecked: "[ ]",
		GlyphComment:   "+",
		GlyphMoreAbove: "^",
		GlyphMoreBelow: "v",
		GlyphReviewed:  "x",
		GlyphDraft:     "*",
	},
	GlyphsUnicode: {
		GlyphOpen:           "\u25bc", // black down-pointing triangle
		GlyphFolded:         "\u25b6", // black right-pointing triangle
		GlyphChecked:        "\u2611", // ballot box with check
		GlyphUnchecked:      "\u2610", // ballot box
		GlyphComment:        "+",
		GlyphMoreAbove:      "\u25b4", // small up triangle
		GlyphMoreBelow:      "\u25be",
		GlyphReviewed:       "\u2713", // check mark
		GlyphDraft:          "\u2022", // bullet
		GlyphStateOpen:      "\u25cb", // white circle
		GlyphStateAddressed: "\u25ce", // bullseye
		GlyphStateResolved:  "\u2713",
		GlyphReply:          "\u21a9", // leftwards arrow with hook
		GlyphResolve:        "\u2713",
		GlyphReopen:         "\u21ba", // anticlockwise open circle arrow
		GlyphAddressed:      "\u25ce",
		GlyphEdit:           "\u270e", // lower right pencil
		GlyphDelete:         "\u2717", // ballot x
		GlyphSave:           "\u2713",
		GlyphCancel:         "\u2717",
		GlyphEditor:         "\u270e",
		GlyphPrev:           "\u2039", // single left angle quote
		GlyphNext:           "\u203a", // single right angle quote
		GlyphApprove:        "\u2713",
		GlyphRequestChanges: "\u2717",
		GlyphYes:            "\u2713",
		GlyphNo:             "\u2717",
	},
	GlyphsNerd: {
		// The triangles, not Font Awesome's chevrons, which are drawn wider than their cell and
		// thin, so they spill over what they head and are hard to see.
		GlyphOpen:      "\u25bc",
		GlyphFolded:    "\u25b6",
		GlyphChecked:   "\uf046", // check-square-o
		GlyphUnchecked: "\uf096", // square-o
		// The + that comments stays a +: the icon is drawn wider than its cell, and the line's
		// sign beside it is a + too.
		GlyphComment:        "+",
		GlyphMoreAbove:      "\uf077", // chevron-up
		GlyphMoreBelow:      "\uf078",
		GlyphReviewed:       "\uf00c", // check
		GlyphDraft:          "\uf040", // pencil
		GlyphStateOpen:      "\uf10c", // circle-o
		GlyphStateAddressed: "\uf192", // dot-circle-o
		GlyphStateResolved:  "\uf058", // check-circle
		GlyphReply:          "\uf112", // reply
		GlyphResolve:        "\uf00c",
		GlyphReopen:         "\uf0e2", // undo
		GlyphAddressed:      "\uf192",
		GlyphEdit:           "\uf040",
		GlyphDelete:         "\uf1f8", // trash
		GlyphSave:           "\uf0c7", // floppy-o
		GlyphCancel:         "\uf00d", // times
		GlyphEditor:         "\uf044", // pencil-square-o
		GlyphPrev:           "\uf053", // chevron-left
		GlyphNext:           "\uf054",
		GlyphFilter:         "\uf0b0", // filter
		GlyphApprove:        "\uf00c",
		GlyphRequestChanges: "\uf071", // exclamation-triangle
		GlyphVerdictComment: "\uf075", // comment
		GlyphYes:            "\uf00c",
		GlyphNo:             "\uf00d",
	},
}

// SetGlyphs makes the theme draw with g.
func (t *Theme) SetGlyphs(g Glyphs) { t.glyphs = g }

// Glyph is how the theme draws g.
func (t *Theme) Glyph(g Glyph) string { return glyphSets[t.glyphs][g] }

// glyph is a span drawn as the theme's g, in el. A gap puts a space after it, when it is drawn at
// all, as between a button's icon and its label.
func glyph(el element, g Glyph, gap bool) Span {
	return Span{el: el, glyph: g, gap: gap}
}
