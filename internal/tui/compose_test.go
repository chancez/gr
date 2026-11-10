package tui

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"
	"github.com/guygrigsby/vimbubble/v2"

	"github.com/chancez/gr/internal/anchor"
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
			want = mw - codeIndent(e.m.views[e.m.current], row{file: 1}, false, anchor.SideNew, mw) - 1
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

// In a split diff, a thread on the new side, and the comment box that starts one, hang off the new
// side's code rather than the old side's.
func TestSplitBoxesOnTheNewSide(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("say hello")
	e.pressRun("l")
	e.press("s")
	e.cursorTo(func(r row) bool {
		return r.kind == rowLine && r.right != nil && strings.Contains(r.right.Text, `println("hello")`)
	})
	e.m.main.side = anchor.SideNew
	lines := strings.Split(e.screen(), "\n")
	code, box := -1, -1
	for i, l := range lines {
		if j := strings.Index(l, `println("hello")`); j >= 0 && code < 0 {
			// Where the new side's code starts: its indent, before println.
			code = strings.LastIndex(l[:j], "+") + 2
			for box = i + 1; box < len(lines) && !strings.Contains(lines[box], "+----"); box++ {
			}
		}
	}
	if code < 0 || box >= len(lines) {
		t.Fatalf("no new-side line and thread box:\n%s", e.screen())
	}
	if got := strings.Index(lines[box], "+----"); got != code {
		t.Errorf("the thread's box starts at column %d, want the new side's code at %d:\n%s", got, code, e.screen())
	}
	// Too narrow for its buttons in its border, the box has them inside, and they work there.
	for i, r := range e.m.main.rows {
		if r.kind == rowThread && r.thread.Seq == 1 {
			e.click("row"+strconv.Itoa(i)+":", "reply")
		}
	}
	if e.m.mode != modeCompose || e.m.compose.kind != composeReply {
		t.Fatalf("Reply in a split diff's box left mode %d", e.m.mode)
	}
	e.press("esc")
	e.press("c")
	for i, l := range strings.Split(e.screen(), "\n") {
		if strings.Contains(l, "Comment on main.go:4") {
			above := strings.Split(e.screen(), "\n")[i-1]
			if got := strings.Index(above, "+----"); got != code {
				t.Errorf("the comment box starts at column %d, want %d:\n%s", got, code, e.screen())
			}
		}
	}
}

// Shift held with enter, backspace, and the like does what the plain key does while typing, with
// vim and without; with the arrows it selects, as the text area has it.
func TestShiftedKeysInTheCommentBox(t *testing.T) {
	shifted := func(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code, Mod: tea.ModShift} }
	var got []string
	for _, vim := range []bool{false, true} {
		e := newTUIEnv(t)
		e.m.vim = vim
		e.openNav("say hello")
		e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
		e.press("c")
		e.typeText("abc")
		// abc, then ab, then b selected, then replaced by a new line.
		for _, k := range []tea.KeyPressMsg{shifted(tea.KeyBackspace), shifted(tea.KeyLeft), shifted(tea.KeyEnter)} {
			e.m.Update(k)
		}
		e.typeText("d")
		got = append(got, e.m.compose.area.Value())
	}
	if diff := cmp.Diff([]string{"a\nd", "a\nd"}, got); diff != "" {
		t.Errorf("text, without vim then with (-want +got):\n%s", diff)
	}
}

// With vim, the terminal's cursor is right after what was typed: in a diff, a split diff, and the
// overview, whose rows are drawn after a margin.
func TestComposeCursorAfterTheText(t *testing.T) {
	type where struct {
		name string
		open func(e *tuiEnv)
	}
	var got []string
	for _, w := range []where{
		{"diff", func(e *tuiEnv) {
			e.openNav("say hello")
			e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
			e.press("c")
		}},
		{"split", func(e *tuiEnv) {
			e.openNav("say hello")
			e.press("s")
			e.cursorTo(func(r row) bool {
				return r.kind == rowLine && r.right != nil && strings.Contains(r.right.Text, "hello")
			})
			e.m.main.side = anchor.SideNew
			e.press("c")
		}},
		{"overview", func(e *tuiEnv) {
			e.openNav("Overview")
			e.cursorTo(func(r row) bool { return r.kind == rowThread && !r.collapsed })
			e.press("r")
		}},
	} {
		e := goldenEnv(t)
		e.m.vim = true
		w.open(e)
		e.typeText("typed")
		c := e.m.composeCursor()
		if c == nil {
			t.Fatalf("%s: no cursor:\n%s", w.name, e.screen())
		}
		line := ansi.Strip(strings.Split(e.m.render(), "\n")[c.Y])
		end := -1
		if at := strings.Index(line, "typed"); at >= 0 {
			end = ansi.StringWidth(line[:at]) + len("typed")
		}
		got = append(got, fmt.Sprintf("%s: cursor at %d, text ends at %d", w.name, c.X, end))
		want := fmt.Sprintf("%s: cursor at %d, text ends at %d", w.name, end, end)
		if got[len(got)-1] != want {
			t.Errorf("%s, want %s; line %q", got[len(got)-1], want, line)
		}
	}
}
