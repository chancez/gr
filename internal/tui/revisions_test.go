package tui

import (
	"testing"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
)

// p picks two revisions, and the interdiff compares them, rather than the latest with the one
// before it, and keeps comparing them across a reload.
func TestCompareRevisions(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	for _, text := range []string{"hello, world", "hello, there"} {
		e.repo.Write("main.go", gittest.Lines("package main", "", "func main() {", "\tprintln(\""+text+"\")", "}"))
		e.repo.Commit("say " + text)
		if _, _, err := e.svc.Sync(ctx, e.chg); err != nil {
			t.Fatal(err)
		}
	}
	e.drain(e.m.loadCmd())
	e.press("p")
	e.typeText("rev 1")
	e.pressRun("enter")
	if e.m.mode != modePicker || e.m.picker.title != "Compare revision 1 to" {
		t.Fatalf("after picking rev 1: mode %d, picker %q", e.m.mode, e.m.picker.title)
	}
	e.typeText("rev 2")
	e.pressRun("enter")
	got := func() (string, string) {
		v := e.m.views[e.m.current]
		var text string
		for _, f := range v.files {
			for _, h := range f.Hunks {
				for _, l := range h.Lines {
					text += map[git.LineKind]string{git.LineContext: " ", git.LineAdded: "+", git.LineDeleted: "-"}[l.Kind] + l.Text + "\n"
				}
			}
		}
		return e.m.viewLabel(v), text
	}
	label, text := got()
	want := "Rev 1 to 2"
	wantText := gittest.Lines(" package main", " ", " func main() {", "-\tprintln(\"hello\")", "+\tprintln(\"hello, world\")", " }")
	if label != want || text != wantText {
		t.Errorf("interdiff %q:\n%s\nwant %q:\n%s", label, text, want, wantText)
	}
	e.drain(e.m.loadCmd())
	if label, text := got(); label != want || text != wantText {
		t.Errorf("after a reload, interdiff %q:\n%s\nwant %q", label, text, want)
	}
}
