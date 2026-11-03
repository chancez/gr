package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	y := e.m.main.starts[e.m.main.cursor] - e.m.main.offset + 1
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
