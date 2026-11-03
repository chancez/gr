package look

import (
	"reflect"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/cells"
)

func TestEveryElementAndSurfaceIsNamed(t *testing.T) {
	for i, n := range elementNames {
		if n == "" {
			t.Errorf("element %d has no name", i)
		}
	}
	for i, n := range surfaceNames {
		if n == "" {
			t.Errorf("surface %d has no name", i)
		}
	}
}

func TestColorblindTheme(t *testing.T) {
	for _, dark := range []bool{true, false} {
		ld := lipgloss.LightDark(dark)
		// Blue and orange stand in for green and red, by role.
		want := newRoles(dark, ThemeDefault)
		want.success, want.added = want.accent, want.accent
		want.danger, want.deleted = ld(orange, lightOrange), ld(orange, lightOrange)
		want.addedBg, want.deletedBg = ld(paleSky, deepNavy), ld(paleOrange, darkBrown)
		if got := newRoles(dark, ThemeColorblind); !reflect.DeepEqual(got, want) {
			t.Errorf("dark %v: colorblind roles = %+v, want %+v", dark, got, want)
		}
	}
}

// Each value is drawn as its own element, in its own format.
func TestValues(t *testing.T) {
	for name, tc := range map[string]struct {
		got  []Span
		want []Span
	}{
		"short sha": {got: []Span{SHA("0123456789abcdef").Short()}, want: []Span{{el: elSHA, text: "0123456"}}},
		"verdict":   {got: []Span{Verdict(store.VerdictRequestChanges)}, want: []Span{{el: elVerdictRequestChanges, text: "requested changes"}}},
		"location": {
			got:  Location(store.Location{Scope: store.ScopeCommit, Commit: "0123456789", Path: "a.go", Side: anchor.SideOld, StartLine: 2, EndLine: 4}),
			want: []Span{{el: elMuted, text: "commit "}, {el: elSHA, text: "0123456"}, {el: elMuted, text: " a.go:2-4 (old)"}},
		},
		"location as a heading": {
			got:  Location(store.Location{Scope: store.ScopeDiff, Path: "a.go"}, Strong),
			want: []Span{{el: elPath, text: "a.go"}},
		},
		"gutter":   {got: []Span{Gutter(3, 0, 12)}, want: []Span{{el: elGutter, text: "     12 "}}},
		"badge":    {got: Badge(2, true, true), want: []Span{{el: elCount, text: "(2)"}, {el: elDefault, text: " "}, {el: elDraft, text: "*", props: Strong}, {el: elReviewed, text: "x", props: Strong}}},
		"no badge": {got: Badge(0, false, false), want: nil},
		"file": {
			got:  []Span{File(git.FileDiff{OldPath: "a.go", NewPath: "b.go", Status: git.StatusRenamed})},
			want: []Span{{el: elPath, text: " R a.go -> b.go"}},
		},
	} {
		if !reflect.DeepEqual(tc.got, tc.want) {
			t.Errorf("%s = %+v, want %+v", name, tc.got, tc.want)
		}
	}
}

func TestThemeDraw(t *testing.T) {
	th := New(true, ThemeDefault)
	p := th.roles
	bg := lipgloss.NewStyle().Background(p.cursorBg)
	for name, tc := range map[string]struct {
		got  string
		want string
	}{
		"soft surface keeps element colors and pads with its background": {
			got:  th.Draw(Cursor, 8, Author(store.Author{Name: "Ann"}), Muted(" x")),
			want: bg.Bold(true).Foreground(lavender).Render("Ann") + bg.Foreground(p.muted).Render(" x") + bg.Foreground(p.text).Render("   "),
		},
		"solid surface draws every element in its own foreground": {
			got:  th.Render(PaneFocused, Author(store.Author{Name: "Ann"})),
			want: lipgloss.NewStyle().Foreground(p.onSolid).Background(p.accent).Bold(true).Render("Ann"),
		},
		"dim mutes and unbolds": {
			got:  th.Render(Plain, Author(store.Author{Name: "Ann"}).With(Dim)),
			want: lipgloss.NewStyle().Foreground(p.muted).Render("Ann"),
		},
		"text is made safe": {
			got:  th.Render(Plain, Text("a\x1b[5m")),
			want: lipgloss.NewStyle().Foreground(p.text).Render("a^[[5m"),
		},
		"cut marks the cut on the surface": {
			got:  th.DrawCut(Cursor, 5, Text("abcdefgh")),
			want: bg.Foreground(p.text).Render("ab") + bg.Foreground(p.text).Render("..."),
		},
	} {
		if !reflect.DeepEqual(cells.Parse(tc.got), cells.Parse(tc.want)) {
			t.Errorf("%s:\ngot  %q\nwant %q", name, tc.got, tc.want)
		}
	}
}

func TestZones(t *testing.T) {
	th := New(true, ThemeDefault)
	line := th.Render(Plain, append(append([]Span{Text("ab ")}, MarkSHA("commit:0123456789", SHA("0123456789").Short())...), Text(" cd"))...)
	frame := "first line\n" + line + "\n" + th.Draw(Plain, 6, append([]Span{Text("xy ")}, MarkSHA("cut", SHA("0123456789").Short())...)...)
	clean, zones := Scan(frame)
	if strings.Contains(clean, "grz") {
		t.Errorf("Scan left marks in %q", clean)
	}
	want := map[string][]Zone{"commit:0123456789": {{X: 3, Y: 1, W: 7}}, "cut": {{X: 3, Y: 2, W: 3}}}
	if !reflect.DeepEqual(zones, want) {
		t.Errorf("zones = %+v, want %+v", zones, want)
	}
}

// Restyling marked text keeps its marks, and so its zones.
func TestKeepMarks(t *testing.T) {
	th := New(true, ThemeDefault)
	line := th.Render(Plain, append(append([]Span{Text("ab ")}, MarkSHA("sha", SHA("0123456789").Short())...), Text(" cd"))...)
	// Restyle "ab 01", cutting through the marked sha.
	restyled := KeepMarks(line, func(s string) string {
		return th.Render(SearchMatch, Text(ansi.Strip(ansi.Cut(s, 0, 5)))) + ansi.Cut(s, 5, ansi.StringWidth(s))
	})
	_, zones := Scan(restyled)
	if want := map[string][]Zone{"sha": {{X: 3, Y: 0, W: 7}}}; !reflect.DeepEqual(zones, want) {
		t.Errorf("zones = %+v, want %+v", zones, want)
	}
}
