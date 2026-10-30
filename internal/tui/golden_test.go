package tui

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
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
	gotCells, wantCells := cells(got), cells(string(want))
	for y := range max(len(gotCells), len(wantCells)) {
		var g, w []cell
		if y < len(gotCells) {
			g = gotCells[y]
		}
		if y < len(wantCells) {
			w = wantCells[y]
		}
		for x := range max(len(g), len(w)) {
			var gc, wc cell
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

// cell is one character on screen and how it is drawn.
type cell struct {
	r     rune
	style sgr
}

// sgr is the drawing state that SGR escapes set. Colors keep their parameters, such as "38;2;1;2;3".
type sgr struct {
	fg        string
	bg        string
	bold      bool
	faint     bool
	italic    bool
	underline bool
	reverse   bool
	strike    bool
}

// cells reads a screen into lines of cells, so two screens compare by how they look rather than by
// the escapes that draw them: a style can be set in one escape or in several.
func cells(screen string) [][]cell {
	var out [][]cell
	for line := range strings.SplitSeq(screen, "\n") {
		var st sgr
		var row []cell
		for i := 0; i < len(line); {
			if rest, ok := strings.CutPrefix(line[i:], "\x1b["); ok {
				end := strings.IndexByte(rest, 'm')
				st.apply(strings.Split(rest[:end], ";"))
				i += 2 + end + 1
				continue
			}
			r, n := utf8.DecodeRuneInString(line[i:])
			c := cell{r: r, style: st}
			if r == ' ' {
				// A space shows only its background and lines through it, not its text's color.
				c.style = sgr{bg: st.bg, underline: st.underline, reverse: st.reverse, strike: st.strike}
			}
			row = append(row, c)
			i += n
		}
		out = append(out, row)
	}
	return out
}

func (s *sgr) apply(params []string) {
	for i := 0; i < len(params); i++ {
		switch p := params[i]; p {
		case "", "0":
			*s = sgr{}
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
