package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"

	"github.com/chancez/gr/internal/tui/interact"
)

// TestPlacesComeFromTheRegistry enforces ADR 10: only registry.go makes kinds and places, so a
// subject cannot be put somewhere that was never registered.
func TestPlacesComeFromTheRegistry(t *testing.T) {
	makers := map[string]bool{"NewKind": true, "List": true, "Mention": true, "Via": true}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if name == "registry.go" || strings.HasSuffix(name, "_test.go") {
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
			if pkg, _ := sel.X.(*ast.Ident); pkg != nil && pkg.Name == "interact" && makers[sel.Sel.Name] {
				t.Errorf("%s: interact.%s outside registry.go; register kinds and places there", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}

// Every kind and place with a described key is in a section of help, so a key cannot be bound
// where help never shows it.
func TestHelpShowsEveryKey(t *testing.T) {
	inHelp := map[interact.Keyed]bool{}
	for _, sec := range helpOrder() {
		for _, k := range sec.from {
			inHelp[k] = true
		}
	}
	for i, k := range interact.Made() {
		described := slices.ContainsFunc(k.Bindings(), func(b key.Binding) bool { return b.Help().Desc != "" })
		if described && !inHelp[k] {
			t.Errorf("kind or place #%d, with keys %+v, is in no section of help", i, k.Bindings())
		}
	}
}
