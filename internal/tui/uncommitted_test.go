package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
)

// uncommittedShown is what the uncommitted view shows: its sidebar entry, and each file's path
// with its added lines.
func (e *tuiEnv) uncommittedShown() []string {
	e.t.Helper()
	var out []string
	for _, v := range e.m.views {
		if v.kind != viewUncommitted {
			continue
		}
		out = append(out, e.m.viewLabel(v))
		for _, f := range v.files {
			added := []string{f.Path()}
			for _, h := range f.Hunks {
				for _, l := range h.Lines {
					if l.Kind == git.LineAdded {
						added = append(added, l.Text)
					}
				}
			}
			out = append(out, strings.Join(added, " "))
		}
	}
	return out
}

// What the branch's worktree has uncommitted shows after its commits, follows edits while it is
// shown, takes no comments or marks, and goes once there is nothing uncommitted.
func TestUncommitted(t *testing.T) {
	e := newTUIEnv(t)
	if got := e.uncommittedShown(); got != nil {
		t.Errorf("clean worktree shows %q", got)
	}

	e.repo.Write("main.go", gittest.Lines("package main", "", "func main() {", "\tprintln(\"hey\")", "}"), "new.go", "package main\n")
	e.tick()
	if !strings.Contains(e.screen(), "Uncommitted: 2 files, +2 -1") {
		t.Errorf("overview does not count what is uncommitted:\n%s", e.screen())
	}
	e.openNav("Uncommitted")
	e.press("l")
	want := []string{"Uncommitted (2 files)", "main.go \tprintln(\"hey\")", "new.go package main"}
	if diff := cmp.Diff(want, e.uncommittedShown()); diff != "" {
		t.Errorf("uncommitted (-want +got):\n%s", diff)
	}

	e.cursorTo(addedLine)
	for _, k := range []string{"c", "C", "m"} {
		e.m.status = ""
		e.press(k)
		if e.m.mode != modeNormal || !strings.Contains(e.m.status, "uncommitted changes are read-only") {
			t.Errorf("%s on what is uncommitted: mode %d, status %q", k, e.m.mode, e.m.status)
		}
	}

	// Changed again, a file changes nothing git's status says, but what is shown follows it.
	e.repo.Write("main.go", gittest.Lines("package main", "", "func main() {", "\tprintln(\"hey you\")", "}"))
	e.tick()
	want = []string{"Uncommitted (2 files)", "main.go \tprintln(\"hey you\")", "new.go package main"}
	if diff := cmp.Diff(want, e.uncommittedShown()); diff != "" {
		t.Errorf("uncommitted after an edit (-want +got):\n%s", diff)
	}

	e.repo.Git("checkout", "--", "main.go")
	if err := os.Remove(filepath.Join(e.repo.Dir, "new.go")); err != nil {
		t.Fatal(err)
	}
	e.tick()
	if got := e.uncommittedShown(); got != nil {
		t.Errorf("worktree cleaned shows %q", got)
	}
}
