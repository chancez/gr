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
