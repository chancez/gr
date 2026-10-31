package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"

	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/look"
)

// TestStylesComeFromTheRegistry enforces ADR 8: only the look package creates styles or sets colors.
// Syntax highlighting is a foreign renderer, styled by its own theme.
func TestStylesComeFromTheRegistry(t *testing.T) {
	allowed := map[string]bool{"highlight.go": true}
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
				t.Errorf("%s: %s outside the registry; add an element and a constructor to the look package", fset.Position(sel.Pos()), sel.Sel.Name)
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
	light, dark := look.New(false, ThemeDefault), look.New(true, ThemeDefault)
	want := light.Render(look.HeadingBar, look.Verdict(store.VerdictRequestChanges))
	stale := dark.Render(look.HeadingBar, look.Verdict(store.VerdictRequestChanges))
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
	dimHeader := th.Render(look.HeadingBar, look.Heading("Review #1").With(look.Dim))
	header := th.Render(look.HeadingBar, look.Heading("Review #1"))
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
