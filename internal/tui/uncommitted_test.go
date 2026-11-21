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

// U shows all changes with what is uncommitted on top, read-only and without the head's threads,
// and again only what is committed.
func TestAllChangesWithUncommitted(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("All changes")
	e.press("l")
	e.cursorTo(addedLine)
	e.press("c")
	e.typeText("on the head")
	e.pressRun("ctrl+s")
	e.repo.Write("main.go", gittest.Lines("package main", "", "func main() {", "\tprintln(\"hey\")", "}"), "new.go", "package main\n")
	e.tick()

	type shown struct {
		Label   string
		Added   []string
		Threads int
		Status  string
	}
	look := func() shown {
		v := e.m.views[e.m.current]
		s := shown{Label: e.m.viewLabel(v)}
		for _, f := range v.files {
			for _, h := range f.Hunks {
				for _, l := range h.Lines {
					if l.Kind == git.LineAdded {
						s.Added = append(s.Added, l.Text)
					}
				}
			}
		}
		for i := range e.m.data.threads {
			if _, ok := e.m.data.locationIn(&e.m.data.threads[i], v); ok {
				s.Threads++
			}
		}
		e.m.status = ""
		e.cursorTo(addedLine)
		e.press("c")
		s.Status = e.m.status
		e.press("esc")
		return s
	}
	committed := shown{Label: "All changes (1 file)", Added: []string{"\tprintln(\"hello\")"}, Threads: 1}
	if diff := cmp.Diff(committed, look()); diff != "" {
		t.Errorf("all changes (-want +got):\n%s", diff)
	}
	e.press("U")
	want := shown{
		Label:  "With uncommitted (2 files)",
		Added:  []string{"\tprintln(\"hey\")", "package main"},
		Status: "uncommitted changes are read-only; U shows only what is committed",
	}
	if diff := cmp.Diff(want, look()); diff != "" {
		t.Errorf("all changes with uncommitted (-want +got):\n%s", diff)
	}
	e.press("U")
	if diff := cmp.Diff(committed, look()); diff != "" {
		t.Errorf("all changes again (-want +got):\n%s", diff)
	}
}
