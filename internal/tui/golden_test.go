package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/cells"
)

var update = flag.Bool("update", false, "rewrite the golden screens in testdata/golden")

// goldenEnv is a change with something of everything to draw: two commits, an added and a modified
// file, open, addressed, resolved, and draft threads, a Markdown body, and a submitted review.
func goldenEnv(t *testing.T) *tuiEnv {
	t.Helper()
	local := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = local })
	now := gittest.Epoch
	clock := func() time.Time {
		now = now.Add(time.Minute)
		return now
	}
	e := newTUIEnv(t, store.WithClock(clock))
	ctx := t.Context()
	first := strings.TrimSpace(e.repo.Git("rev-parse", "HEAD"))
	e.repo.Write("util.go", gittest.Lines("package main", "", "// greet says hello.", "func greet(name string) string {",
		"\treturn \"hello, \" + name", "}"))
	e.repo.Write("main.go", gittest.Lines("package main", "", "func main() {", "\tprintln(greet(\"world\"))", "}"))
	e.repo.Commit("Add a greeting helper")
	if _, _, err := e.svc.Sync(ctx, e.chg); err != nil {
		t.Fatal(err)
	}
	start := func(nt review.NewThread) review.ThreadView {
		t.Helper()
		tv, err := e.svc.StartThread(ctx, e.chg, nt)
		if err != nil {
			t.Fatal(err)
		}
		return tv
	}
	start(review.NewThread{Scope: store.ScopeCommit, Commit: first, Path: "main.go", StartLine: 4, Body: "Say hello to someone."})
	addressed := start(review.NewThread{Scope: store.ScopeDiff, Path: "util.go", StartLine: 4, EndLine: 5,
		Body: "Return a **formatted** string:\n\n```go\nfmt.Sprintf(\"hello, %s\", name)\n```"})
	resolved := start(review.NewThread{Scope: store.ScopeCommit, Commit: first, Path: review.CommitMsgPath, StartLine: 1,
		Body: "Capitalize the subject."})
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictRequestChanges, "Close. A few things to fix."); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Reply(ctx, e.chg, addressed.ID, "Done.", false); err != nil {
		t.Fatal(err)
	}
	for id, state := range map[string]store.ThreadState{addressed.ID: store.ThreadAddressed, resolved.ID: store.ThreadResolved} {
		if err := e.svc.SetThreadState(ctx, id, state); err != nil {
			t.Fatal(err)
		}
	}
	start(review.NewThread{Scope: store.ScopeChange, Body: "One more thought."})
	e.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	e.drain(e.m.loadCmd())
	return e
}

// golden compares the screen, with its colors, to testdata/golden/<name>.ansi, or rewrites the file
// with -update.
func (e *tuiEnv) golden(name string) {
	e.t.Helper()
	got := e.m.render()
	path := filepath.Join("testdata", "golden", name+".ansi")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			e.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			e.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatalf("%v; run with -update to create it", err)
	}
	gotCells, wantCells := cells.Parse(got), cells.Parse(string(want))
	for y := range max(len(gotCells), len(wantCells)) {
		var g, w []cells.Cell
		if y < len(gotCells) {
			g = gotCells[y]
		}
		if y < len(wantCells) {
			w = wantCells[y]
		}
		for x := range max(len(g), len(w)) {
			var gc, wc cells.Cell
			if x < len(g) {
				gc = g[x]
			}
			if x < len(w) {
				wc = w[x]
			}
			if gc != wc {
				e.t.Fatalf("%s: line %d column %d differs\ngot:  %+v\nwant: %+v\nscreen:\n%s", name, y+1, x+1, gc, wc, e.screen())
			}
		}
	}
}

// TestGoldenScreens pins how every pane looks, colors included, so a change to drawing that should
// look the same can be checked to.
func TestGoldenScreens(t *testing.T) {
	for _, theme := range []string{"dark", "light"} {
		t.Run(theme, func(t *testing.T) {
			e := goldenEnv(t)
			background := lipgloss.Color("#ffffff")
			if theme == "dark" {
				background = lipgloss.Color("#000000")
			}
			e.m.Update(tea.BackgroundColorMsg{Color: background})

			e.openNav("Overview")
			e.golden(theme + "-overview")
			e.pressRun("l")
			e.golden(theme + "-overview-focused")
			e.press("j", "j")
			e.golden(theme + "-overview-moved")

			e.press("h")
			e.openNav("say hello")
			e.golden(theme + "-commit")
			e.pressRun("l")
			e.cursorTo(func(r row) bool { return addedLine(r) && strings.Contains(r.line.Text, "println") })
			e.golden(theme + "-commit-cursor")
			e.press("v", "k")
			e.golden(theme + "-commit-selection")
			e.press("esc")

			e.press("h")
			e.openNav("All changes")
			e.pressRun("l")
			e.cursorTo(func(r row) bool { return r.kind == rowFile })
			e.pressRun("m")
			e.golden(theme + "-diff-reviewed")
			e.press("s")
			e.cursorTo(func(r row) bool { return r.kind == rowLine && r.right != nil })
			e.golden(theme + "-diff-split")
			e.press("?")
			e.golden(theme + "-help")
		})
	}
	t.Run("colorblind", func(t *testing.T) {
		e := goldenEnv(t)
		e.m.theme = ThemeColorblind
		e.m.Update(tea.BackgroundColorMsg{Color: lipgloss.Color("#000000")})
		e.openNav("All changes")
		e.golden("colorblind-diff")
	})
}
