package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"

	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

func TestThemeDraw(t *testing.T) {
	th := newTheme(true, ThemeDefault)
	p := th.roles
	bg := lipgloss.NewStyle().Background(p.cursorBg)
	for name, tc := range map[string]struct {
		got  string
		want string
	}{
		"soft surface keeps element colors and pads with its background": {
			got:  th.draw(surfCursor, 8, sp(elAuthor, "Ann"), sp(elMuted, " x")),
			want: bg.Bold(true).Foreground(lavender).Render("Ann") + bg.Foreground(p.muted).Render(" x") + bg.Foreground(p.text).Render("   "),
		},
		"solid surface draws every element in its own foreground": {
			got:  th.render(surfPaneFocused, sp(elAuthor, "Ann")),
			want: lipgloss.NewStyle().Foreground(p.onSolid).Background(p.accent).Bold(true).Render("Ann"),
		},
		"dim mutes and unbolds": {
			got:  th.render(surfPlain, sp(elAuthor, "Ann", dim)),
			want: lipgloss.NewStyle().Foreground(p.muted).Render("Ann"),
		},
		"text is made safe": {
			got:  th.render(surfPlain, sp(elText, "a\x1b[5m")),
			want: lipgloss.NewStyle().Foreground(p.text).Render("a^[[5m"),
		},
		"cut marks the cut on the surface": {
			got:  th.drawCut(surfCursor, 5, sp(elText, "abcdefgh")),
			want: bg.Foreground(p.text).Render("ab") + bg.Foreground(p.text).Render("..."),
		},
	} {
		if !reflect.DeepEqual(cells(tc.got), cells(tc.want)) {
			t.Errorf("%s:\ngot  %q\nwant %q", name, tc.got, tc.want)
		}
	}
}

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

// TestStylesComeFromTheRegistry enforces ADR 8: only the registry creates styles or sets colors.
// Syntax highlighting is a foreign renderer, styled by its own theme.
func TestStylesComeFromTheRegistry(t *testing.T) {
	allowed := map[string]bool{"palette.go": true, "theme.go": true, "draw.go": true, "highlight.go": true}
	// lipgloss functions that make styles and colors, and style methods that set colors.
	constructors := map[string]bool{"NewStyle": true, "Color": true, "LightDark": true}
	setters := map[string]bool{"Foreground": true, "Background": true, "BorderForeground": true, "BorderBackground": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if allowed[name] || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, _ := sel.X.(*ast.Ident)
			if setters[sel.Sel.Name] || constructors[sel.Sel.Name] && pkg != nil && pkg.Name == "lipgloss" {
				t.Errorf("%s: %s outside the registry; add an element or surface to theme.go", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}

// A light background, learned after the screen is first drawn, recolors what is already built.
func TestLightBackgroundRecolorsBuiltRows(t *testing.T) {
	e := newTUIEnv(t)
	if _, err := e.svc.Submit(t.Context(), e.chg, store.VerdictRequestChanges, "fix"); err != nil {
		t.Fatal(err)
	}
	e.drain(e.m.loadCmd())
	e.openNav("Overview")
	e.m.Update(tea.BackgroundColorMsg{Color: lipgloss.Color("#ffffff")})
	light, dark := newTheme(false, ThemeDefault), newTheme(true, ThemeDefault)
	want := light.render(surfHeading, sp(elVerdictRequestChanges, "requested changes"))
	stale := dark.render(surfHeading, sp(elVerdictRequestChanges, "requested changes"))
	if out := e.m.render(); !strings.Contains(out, want) || strings.Contains(out, stale) {
		t.Errorf("the overview keeps the dark theme's colors on a light background:\n%q", out)
	}
}

// A review whose threads are all resolved recedes, and one with an open thread stands out.
func TestFinishedReviewsDim(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	tv, err := e.svc.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeChange, Body: "done soon"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictRequestChanges, ""); err != nil {
		t.Fatal(err)
	}
	th := e.m.r.th
	dimHeader := th.render(surfHeading, sp(elHeading, "Review #1", dim))
	header := th.render(surfHeading, sp(elHeading, "Review #1"))
	for _, tc := range []struct {
		state store.ThreadState
		want  string
	}{
		{store.ThreadOpen, header},
		{store.ThreadResolved, dimHeader},
	} {
		if err := e.svc.SetThreadState(ctx, tv.ID, tc.state); err != nil {
			t.Fatal(err)
		}
		e.drain(e.m.loadCmd())
		e.openNav("Overview")
		if !strings.Contains(e.m.render(), tc.want) {
			t.Errorf("thread %s: review header is not %q:\n%s", tc.state, tc.want, e.screen())
		}
	}
}
