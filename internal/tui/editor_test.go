package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/gittest"
)

// E opens the file as it is in the view, at the cursor's line, in a read-only copy that is removed
// after: the old side's as it was before the commit, and the new side's as the commit has it.
func TestEditorCommand(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	v := e.m.views[e.m.current]
	f := -1
	for i, file := range v.files {
		if file.Path() == "main.go" {
			f = i
		}
	}
	out := filepath.Join(t.TempDir(), "out")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", `f() { echo "$1" >`+out+`; { test -w "$2" && echo writable || echo read-only; } >>`+out+`; basename "$2" >>`+out+`; cat "$2" >>`+out+`; }; f`)
	for _, tc := range []struct {
		side anchor.Side
		want string
	}{
		{anchor.SideNew, "+4\nread-only\n" + shortSHA(v.commit.SHA) + "-main.go\n" + gittest.Lines("package main", "", "func main() {", "\tprintln(\"hello\")", "}")},
		{anchor.SideOld, "+4\nread-only\n" + shortSHA(v.commit.SHA) + "-before-main.go\n" + gittest.Lines("package main", "", "func main() {", "\tprintln(\"hi\")", "}")},
	} {
		cmd, done, err := e.m.editorCommand(v, f, tc.side, 4)
		if err != nil {
			t.Fatal(err)
		}
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v", b, err)
		}
		done()
		got, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Errorf("%s side: editor saw\n%q\nwant\n%q", tc.side, got, tc.want)
		}
		if _, err := os.Stat(filepath.Dir(strings.TrimSpace(cmd.Args[len(cmd.Args)-1]))); !os.IsNotExist(err) {
			t.Errorf("%s side: the copy's directory is still there after done: %v", tc.side, err)
		}
	}
}
