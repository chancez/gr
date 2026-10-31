package tui

import (
	"testing"

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
