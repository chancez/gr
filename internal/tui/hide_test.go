package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// hideEnv is a change whose last commit adds a test and a README beside main.go.
func hideEnv(t *testing.T) *tuiEnv {
	t.Helper()
	e := newTUIEnv(t)
	e.repo.Write("main_test.go", gittest.Lines("package main"))
	e.repo.Write("README.md", gittest.Lines("# hi"))
	e.repo.Commit("Add a test and a README")
	if _, _, err := e.svc.Sync(t.Context(), e.chg); err != nil {
		t.Fatal(err)
	}
	e.drain(e.m.loadCmd())
	e.openNav("All changes")
	return e
}

// hideState is what shows of the files: the file list's entries, and the files the diff has.
type hideState struct {
	Files []string
	Diff  []string
}

func (e *tuiEnv) hideState() hideState {
	var s hideState
	for _, it := range e.m.filesNav.items {
		s.Files = append(s.Files, it.prefix+it.label)
	}
	for _, r := range e.m.main.rows {
		if r.kind == rowFile {
			s.Diff = append(s.Diff, e.m.views[e.m.current].files[r.file].Path())
		}
	}
	return s
}

// selectEntry selects the file list's entry labeled label.
func (e *tuiEnv) selectEntry(label string) {
	e.t.Helper()
	e.m.setFocus(focusFiles)
	for i, it := range e.m.filesNav.items {
		if it.label == label {
			e.m.filesNav.pos = i
			return
		}
	}
	e.t.Fatalf("no file list entry %q", label)
}

// - hides a file from the diff, folding it under the file list's hidden entry, in every view; -
// on it there shows it again.
func TestHideFile(t *testing.T) {
	e := hideEnv(t)
	want := hideState{
		Files: []string{"A README.md", "M main.go", "A main_test.go"},
		Diff:  []string{"README.md", "main.go", "main_test.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Fatalf("before hiding (-want +got):\n%s", diff)
	}

	e.selectEntry("main_test.go")
	e.press("-")
	want = hideState{
		Files: []string{"A README.md", "M main.go", "> 1 hidden"},
		Diff:  []string{"README.md", "main.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after - (-want +got):\n%s", diff)
	}

	e.openNav("Add a test")
	want = hideState{Files: []string{"A README.md", "> 1 hidden"}, Diff: []string{"README.md"}}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("in the commit (-want +got):\n%s", diff)
	}

	e.selectEntry("1 hidden")
	e.press("enter")
	e.selectEntry("main_test.go")
	e.press("-")
	want = hideState{
		Files: []string{"A README.md", "A main_test.go"},
		Diff:  []string{"README.md", "main_test.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after - on the hidden file (-want +got):\n%s", diff)
	}
}

// Going to a thread in a hidden file shows the file again.
func TestOpenThreadInHiddenFile(t *testing.T) {
	e := hideEnv(t)
	tv, err := e.svc.StartThread(t.Context(), e.chg, review.NewThread{Scope: store.ScopeDiff, Path: "main_test.go", StartLine: 1, Body: "Test more."})
	if err != nil {
		t.Fatal(err)
	}
	e.drain(e.m.loadCmd())
	e.openNav("All changes")
	e.selectEntry("main_test.go")
	e.press("-")
	e.m.openThread(tv, true)
	r := e.m.main.rows[e.m.main.cursor]
	got := struct {
		Hidden bool
		Thread string
	}{e.m.fileHidden(e.m.views[e.m.current], r.file), r.thread.ID}
	want := struct {
		Hidden bool
		Thread string
	}{false, tv.ID}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("after opening the thread (-want +got):\n%s", diff)
	}
}

// F filters the files: the patterns typed pick the files to show, ! ones hide files, and a new
// filter hides again the files shown by hand. An empty filter shows every file.
func TestFilterFiles(t *testing.T) {
	e := hideEnv(t)
	e.m.setFocus(focusFiles)
	e.press("F")
	e.typeText("*.go !*_test.go")
	e.press("enter")
	want := hideState{
		Files: []string{"M main.go", "> 2 hidden"},
		Diff:  []string{"main.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after filtering (-want +got):\n%s", diff)
	}
	if !strings.Contains(e.screen(), " 1 of 3 Y *.go...") {
		t.Errorf("the file list does not show the filter:\n%s", e.screen())
	}

	e.selectEntry("2 hidden")
	e.press("enter")
	e.selectEntry("README.md")
	e.press("-")
	want = hideState{
		Files: []string{"A README.md", "M main.go", "v 1 hidden", "A main_test.go"},
		Diff:  []string{"README.md", "main.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after unhiding README.md (-want +got):\n%s", diff)
	}

	// The prompt starts from the filter in use.
	e.press("F")
	e.typeText(" !main.go")
	e.press("enter")
	want = hideState{Files: []string{"v 3 hidden", "A README.md", "M main.go", "A main_test.go"}}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after a new filter (-want +got):\n%s", diff)
	}

	e.press("F")
	for range len("*.go !*_test.go !main.go") {
		e.m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	e.press("enter")
	want = hideState{
		Files: []string{"A README.md", "M main.go", "A main_test.go"},
		Diff:  []string{"README.md", "main.go", "main_test.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after clearing the filter (-want +got):\n%s", diff)
	}
}

// v shows the file list's checkboxes, x or a click checks a file, a checks them all, and - hides
// those checked. esc puts the checkboxes away, unchecking them.
func TestPickFiles(t *testing.T) {
	e := hideEnv(t)
	e.m.setFocus(focusFiles)
	e.press("v")
	want := hideState{
		Files: []string{"[ ] A README.md", "[ ] M main.go", "[ ] A main_test.go"},
		Diff:  []string{"README.md", "main.go", "main_test.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after v (-want +got):\n%s", diff)
	}

	e.selectEntry("README.md")
	e.press("x")
	for y, line := range strings.Split(e.screen(), "\n") {
		// Cut at the front to the sidebar's width.
		if strings.Contains(line, "[ ] A ...st.go") {
			e.m.Update(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
		}
	}
	want.Files = []string{"[x] A README.md", "[ ] M main.go", "[x] A main_test.go"}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after x and a click (-want +got):\n%s", diff)
	}
	if got := e.screen(); !strings.Contains(got, "2 selected") {
		t.Errorf("the status line does not count the selected files:\n%s", got)
	}

	e.press("-")
	want = hideState{
		Files: []string{"M main.go", "> 2 hidden"},
		Diff:  []string{"main.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after - (-want +got):\n%s", diff)
	}

	e.press("v", "a")
	want.Files = []string{"[x] M main.go", "> 2 hidden"}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after a (-want +got):\n%s", diff)
	}
	e.press("esc")
	want.Files = []string{"M main.go", "> 2 hidden"}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after esc (-want +got):\n%s", diff)
	}
	if len(e.m.picked) != 0 {
		t.Errorf("picked = %v after esc, want none", e.m.picked)
	}
}

// The file list's Select starts selecting, and the status line's buttons act on the selection.
func TestPickFilesByButton(t *testing.T) {
	e := hideEnv(t)
	e.click("files", "select files")
	// Hide is greyed until a file is selected.
	if got, want := e.shownControls("picking"), []string{"select all", "done selecting"}; !cmp.Equal(got, want) {
		t.Errorf("status line's controls = %q, want %q", got, want)
	}
	e.click("picking", "select all")
	e.click("picking", "hide selected")
	want := hideState{Files: []string{"> 3 hidden"}}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after hiding all (-want +got):\n%s", diff)
	}
	if e.m.picking {
		t.Error("still selecting after hiding the selected files")
	}
}

// fileLine is the screen line the file list shows the entry labeled label on.
func (e *tuiEnv) fileLine(label string) int {
	e.t.Helper()
	e.m.render()
	_, _, bh := e.m.layoutSizes()
	vh, _ := e.m.sidebarHeights(bh)
	for i, it := range e.m.filesNav.items {
		if it.label == label {
			return vh + sidebarGap + e.m.filesNav.head() + i - e.m.filesNav.top
		}
	}
	e.t.Fatalf("no file list entry %q", label)
	return 0
}

// An alt click on a file starts selecting with it checked, and dragging from a box checks or
// unchecks the boxes passed over as the press did the first.
func TestPickFilesByDrag(t *testing.T) {
	e := hideEnv(t)
	e.m.Update(tea.MouseClickMsg{X: 2, Y: e.fileLine("README.md"), Button: tea.MouseLeft, Mod: tea.ModAlt})
	e.m.Update(tea.MouseMotionMsg{X: 2, Y: e.fileLine("main_test.go"), Button: tea.MouseLeft})
	e.m.Update(tea.MouseReleaseMsg{X: 2, Y: e.fileLine("main_test.go"), Button: tea.MouseLeft})
	want := hideState{
		Files: []string{"[x] A README.md", "[x] M main.go", "[x] A main_test.go"},
		Diff:  []string{"README.md", "main.go", "main_test.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after an alt click and a drag (-want +got):\n%s", diff)
	}

	// From a checked box, the drag unchecks.
	e.m.Update(tea.MouseClickMsg{X: 2, Y: e.fileLine("main_test.go"), Button: tea.MouseLeft})
	e.m.Update(tea.MouseMotionMsg{X: 2, Y: e.fileLine("main.go"), Button: tea.MouseLeft})
	e.m.Update(tea.MouseReleaseMsg{X: 2, Y: e.fileLine("main.go"), Button: tea.MouseLeft})
	// Moving after the release checks nothing.
	e.m.Update(tea.MouseMotionMsg{X: 2, Y: e.fileLine("README.md")})
	want.Files = []string{"[x] A README.md", "[ ] M main.go", "[ ] A main_test.go"}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after a drag from a checked box (-want +got):\n%s", diff)
	}
}

// The file list's Filter button opens the filter's prompt, and once there is a filter it is
// labeled with it.
func TestFilterFilesByButton(t *testing.T) {
	e := hideEnv(t)
	e.click("files", "filter files")
	e.typeText("*.md")
	e.press("enter")
	want := hideState{Files: []string{"A README.md", "> 2 hidden"}, Diff: []string{"README.md"}}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after filtering (-want +got):\n%s", diff)
	}
	if got := e.screen(); !strings.Contains(got, " 1 of 3 Y *.md ") {
		t.Errorf("the filter's button is not labeled with it:\n%s", got)
	}
	e.click("files", "filter files")
	if e.m.mode != modeFilter {
		t.Error("a click on the labeled button does not open the prompt")
	}
}

// A click off the filter's prompt closes it without filtering, and does what it does there; a
// click on it leaves it open.
// The filter's prompt takes shift with the editing keys as it takes them alone, and shift+enter
// as enter.
// Hidden files have checkboxes too, under their open entry: - unhides those checked when every
// checked one is hidden.
func TestPickHiddenFiles(t *testing.T) {
	e := hideEnv(t)
	e.m.setFocus(focusFiles)
	e.press("F")
	e.typeText("main.go")
	e.press("enter")
	e.selectEntry("2 hidden")
	e.press("enter", "v")
	e.selectEntry("README.md")
	e.press("x")
	e.selectEntry("main_test.go")
	e.press("x")
	want := hideState{
		Files: []string{"[ ] M main.go", "v 2 hidden", "[x] A README.md", "[x] A main_test.go"},
		Diff:  []string{"main.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after checking hidden files (-want +got):\n%s", diff)
	}
	if got, want := e.shownControls("picking"), []string{"unhide selected", "select all", "done selecting"}; !cmp.Equal(got, want) {
		t.Errorf("status line's controls = %q, want %q", got, want)
	}
	e.press("-")
	want = hideState{
		Files: []string{"A README.md", "M main.go", "A main_test.go"},
		Diff:  []string{"README.md", "main.go", "main_test.go"},
	}
	if diff := cmp.Diff(want, e.hideState()); diff != "" {
		t.Errorf("after - (-want +got):\n%s", diff)
	}
}

// With vim, the filter's prompt edits as the comment box does: esc leaves insert mode, normal
// mode's keys edit, enter runs it in any mode, and esc in normal mode closes it.
