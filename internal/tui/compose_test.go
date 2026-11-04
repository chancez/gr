package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/guygrigsby/vimbubble/v2"

	"github.com/chancez/gr/internal/review"
)

// esc with unsaved text asks before closing: s saves, d discards, and esc keeps editing.
func TestEscAsksBeforeLosingAComment(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
	bodies := func() []string {
		t.Helper()
		threads, err := e.svc.Threads(t.Context(), e.chg, review.ThreadFilter{})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, th := range threads {
			out = append(out, th.Comments[0].Body)
		}
		return out
	}

	e.press("c")
	e.typeText("long thoughts")
	e.press("esc")
	if e.m.mode != modeCompose || !e.m.compose.closing {
		t.Fatalf("esc with text closed the box: mode %d", e.m.mode)
	}
	e.press("esc")
	if e.m.mode != modeCompose || e.m.compose.closing || e.m.compose.area.Value() != "long thoughts" {
		t.Fatalf("second esc did not go back to editing: mode %d, text %q", e.m.mode, e.m.compose.area.Value())
	}
	e.press("esc", "d")
	if e.m.mode != modeNormal || len(bodies()) != 0 {
		t.Fatalf("discarding left mode %d, threads %q", e.m.mode, bodies())
	}

	e.press("c")
	e.typeText("keep this")
	e.press("esc")
	e.pressRun("s")
	if got := bodies(); len(got) != 1 || got[0] != "keep this" {
		t.Fatalf("saving from the prompt gave threads %q, want the draft", got)
	}
	// Commenting there again continues the draft.
	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
	e.press("c")
	if e.m.compose.kind != composeEdit || e.m.compose.area.Value() != "keep this" {
		t.Errorf("commenting on the draft's line opened kind %d with %q, want the draft", e.m.compose.kind, e.m.compose.area.Value())
	}
	// An empty or unchanged box closes without asking.
	e.press("esc")
	if e.m.mode != modeNormal {
		t.Errorf("esc on an unchanged box asked: mode %d", e.m.mode)
	}
}

// The comment box opens under what it comments on, as GitHub's: the sidebar and the lines above
// it stay where they were, and it stays under its line when the rows are built again.
func TestCommentBoxOpensInline(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("say hello")
	e.pressRun("l")
	e.cursorTo(func(r row) bool { return addedLine(r) && strings.Contains(r.line.Text, "println") })
	before := strings.Split(e.screen(), "\n")
	y := e.m.main.starts[e.m.main.cursor] - e.m.main.offset + e.m.mainTop()
	e.press("c")
	e.typeText("Greet someone else.")
	after := strings.Split(e.screen(), "\n")
	nw, _, _ := e.m.layoutSizes()
	for i := range before[:len(before)-1] {
		if i <= y && after[i] != before[i] {
			t.Errorf("line %d moved:\nbefore %q\nafter  %q", i, before[i], after[i])
		}
		if i < len(after) && ansi.Truncate(after[i], nw, "") != ansi.Truncate(before[i], nw, "") {
			t.Errorf("sidebar line %d changed: %q, was %q", i, ansi.Truncate(after[i], nw, ""), ansi.Truncate(before[i], nw, ""))
		}
	}
	box := func() int {
		for i, l := range strings.Split(e.screen(), "\n") {
			if strings.Contains(l, "Comment on main.go:4") {
				return i
			}
		}
		t.Fatalf("no comment box:\n%s", e.screen())
		return 0
	}
	at := box()
	if at <= y {
		t.Errorf("the box is on line %d, above its line %d", at, y)
	}
	e.m.rebuildMain()
	if got := box(); got != at {
		t.Errorf("after the rows were built again, the box moved from line %d to %d", at, got)
	}
	e.press("esc", "d")
	// All but the title bar, which says the comment was discarded.
	if got := strings.Split(e.screen(), "\n"); !reflect.DeepEqual(got[1:], before[1:]) {
		for i := range got {
			if i < len(before) && got[i] != before[i] {
				t.Errorf("after closing, line %d is %q, was %q", i, got[i], before[i])
			}
		}
	}
}

// Near the bottom, the diff scrolls just enough for the box to show under its line, and the box
// comes first where they do not both fit.
func TestCommentBoxNearTheBottom(t *testing.T) {
	for _, tc := range []struct {
		height int
		line   bool
	}{{height: 16, line: false}, {height: 26, line: true}} {
		e := goldenEnv(t)
		e.m.Update(tea.WindowSizeMsg{Width: 120, Height: tc.height})
		e.openNav("say hello")
		e.pressRun("l")
		e.cursorTo(func(r row) bool { return addedLine(r) && strings.Contains(r.line.Text, "println") })
		e.press("c")
		screen := e.screen()
		if got := []bool{strings.Contains(screen, "Save ctrl+s"), strings.Contains(screen, `println("hello")`)}; !reflect.DeepEqual(got, []bool{true, tc.line}) {
			t.Errorf("%d lines high: box and line shown = %v, want [true %v]:\n%s", tc.height, got, tc.line, screen)
		}
	}
}

// With vim, the box opens in insert mode; esc goes to normal mode, where keys are vim's, and esc
// in normal mode is the box's, closing it or asking first. ctrl+s saves from any mode.
func TestVimInTheCommentBox(t *testing.T) {
	e := newTUIEnv(t)
	e.m.vim = true
	e.openNav("say hello")
	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
	e.press("c")
	e.typeText("hello")
	if got := e.m.compose.vim.Mode(); got != vimbubble.Insert || e.m.compose.area.Value() != "hello" {
		t.Fatalf("typing in a new box gave mode %s, text %q", got, e.m.compose.area.Value())
	}
	if e.m.compose.area.Styles().Cursor.Blink {
		t.Error("the cursor blinks in vim mode, want it steady")
	}
	if !strings.Contains(e.screen(), "-- INSERT --") {
		t.Errorf("the box does not say it is in insert mode:\n%s", e.screen())
	}
	e.press("esc")
	if got := e.m.compose.vim.Mode(); e.m.mode != modeCompose || got != vimbubble.Normal {
		t.Fatalf("esc in insert mode left mode %d, vim %s; want the box open in normal mode", e.m.mode, got)
	}
	e.press("0", "x")
	if got := e.m.compose.area.Value(); got != "ello" {
		t.Errorf("0 x in normal mode left %q, want the first letter deleted", got)
	}
	e.press("esc")
	if !e.m.compose.closing {
		t.Fatalf("esc in normal mode with unsaved text did not ask: mode %d", e.m.mode)
	}
	e.press("esc")
	if got := e.m.compose.vim.Mode(); e.m.mode != modeCompose || e.m.compose.closing || got != vimbubble.Normal {
		t.Fatalf("keep editing left mode %d, closing %v, vim %s", e.m.mode, e.m.compose.closing, got)
	}
	e.press("i")
	e.typeText("j")
	e.pressRun("ctrl+s")
	threads, err := e.svc.Threads(t.Context(), e.chg, review.ThreadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if e.m.mode != modeNormal || len(threads) != 1 || threads[0].Comments[0].Body != "jello" {
		t.Errorf("ctrl+s in insert mode left mode %d, threads %+v; want jello saved", e.m.mode, threads)
	}
}

// With vim, the terminal's cursor sits where the text area's is, a bar in insert mode and a block
// in normal mode, steady in both.
func TestVimCursor(t *testing.T) {
	e := newTUIEnv(t)
	e.m.vim = true
	e.openNav("say hello")
	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
	e.press("c")
	e.typeText("hello")
	c := e.m.View().Cursor
	if c == nil {
		t.Fatal("no cursor in vim's insert mode")
	}
	lines := strings.Split(e.screen(), "\n")
	if got := ansi.Cut(lines[c.Y], c.X-5, c.X); got != "hello" || c.Shape != tea.CursorBar || c.Blink {
		t.Errorf("insert cursor at %d,%d shape %d blink %v, after %q; want a steady bar after hello", c.X, c.Y, c.Shape, c.Blink, got)
	}
	e.press("esc")
	if c := e.m.View().Cursor; c == nil || c.Shape != tea.CursorBlock || c.Blink {
		t.Errorf("normal mode cursor = %+v, want a steady block", c)
	}
	e.press("esc", "d")
	if c := e.m.View().Cursor; c != nil {
		t.Errorf("cursor %+v once the box closed", c)
	}
}

// Boxes are capped in width on a wide pane, and on a narrower one fill it but for a column, so
// they always have a border of their own.
func TestBoxWidths(t *testing.T) {
	for _, tc := range []struct {
		width int
		want  int
	}{{width: 300, want: maxBoxWidth}, {width: 120, want: 0}} {
		e := goldenEnv(t)
		e.m.Update(tea.WindowSizeMsg{Width: tc.width, Height: 40})
		e.openNav("say hello")
		_, mw, _ := e.m.layoutSizes()
		want := tc.want
		if want == 0 {
			// The pane's width, less the box's indent and a column of margin.
			want = mw - codeIndent(e.m.views[e.m.current], row{file: 1}, false) - 1
		}
		var got int
		for _, l := range strings.Split(e.screen(), "\n") {
			if i := strings.Index(l, " #1 "); i >= 0 && strings.Contains(l[:i], "| ") {
				i = strings.LastIndex(l[:i], "| ")
				box := l[i:]
				got = strings.LastIndex(box, "|") + 1
			}
		}
		if got != want {
			t.Errorf("%d columns wide: thread box %d wide, want %d", tc.width, got, want)
		}
	}
}
