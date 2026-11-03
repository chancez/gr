package tui

import (
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
	want := []entry{
		{"Changes", "", true},
		{"#1 feature onto main", "current", false},
		{"Branches", "", true},
		{"other", "new change", false},
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
