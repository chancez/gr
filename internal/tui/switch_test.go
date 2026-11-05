package tui

import (
	"path/filepath"
	"reflect"
	"testing"
)

// w lists the open changes and the branches without one, and switches to the one picked, making a
// change for a branch that had none.
func TestSwitchChange(t *testing.T) {
	e := newTUIEnv(t)
	e.repo.Git("checkout", "-q", "-b", "other", "main")
	e.repo.Write("other.go", "package main\n")
	e.repo.Commit("add other")
	e.repo.Git("checkout", "-q", "feature")
	e.press("w")
	if e.m.mode != modePicker {
		t.Fatalf("w opened no picker: mode %d", e.m.mode)
	}
	type entry struct {
		Value   string
		Note    string
		Heading bool
	}
	var got []entry
	for _, it := range e.m.picker.items {
		got = append(got, entry{it.value, it.note, it.heading})
	}
	// Under the repository's name and path.
	want := []entry{
		{filepath.Base(e.repo.Dir), e.svc.Repo().LastPath, true},
		{"#1 feature onto main", "current", false},
		{"other", "branch, new change", false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("picker = %+v, want %+v", got, want)
	}
	e.typeText("other")
	e.pressRun("enter")
	if c := e.m.data.change; c.Branch != "other" || c.Seq != 2 || e.m.views[len(e.m.views)-2].commit.Subject() != "add other" {
		t.Fatalf("after picking other: change #%d %q", c.Seq, c.Branch)
	}
	e.press("w")
	e.typeText("#1")
	e.pressRun("enter")
	if c := e.m.data.change; c.Branch != "feature" || c.Seq != 1 {
		t.Errorf("after picking #1: change #%d %q", c.Seq, c.Branch)
	}
}

// Picking a branch renamed from one with a change opens that change, moved onto the new name.
func TestSwitchToRenamedBranch(t *testing.T) {
	e := newTUIEnv(t)
	e.repo.Git("checkout", "-q", "-b", "other", "main")
	e.repo.Write("other.go", "package main\n")
	e.repo.Commit("add other")
	target, err := e.svc.ParseTarget(t.Context(), "other", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.svc.EnsureChange(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	e.repo.Git("branch", "-m", "other", "renamed")
	e.repo.Git("checkout", "-q", "feature")
	e.press("w")
	e.typeText("renamed")
	e.pressRun("enter")
	type shown struct {
		Seq    int
		Branch string
		Status string
	}
	c := e.m.data.change
	want := shown{Seq: 2, Branch: "renamed", Status: "change #2 follows renamed, renamed from other"}
	if got := (shown{Seq: c.Seq, Branch: c.Branch, Status: e.m.status}); got != want {
		t.Errorf("after picking the renamed branch: %+v, want %+v", got, want)
	}
}
