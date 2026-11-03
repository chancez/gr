package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// B moves the change onto another base without leaving the TUI.
func TestChangeTheBase(t *testing.T) {
	e := newTUIEnv(t)
	e.repo.Git("branch", "parent", "main")
	e.pressRun("B")
	if it, _ := e.m.picker.chosen(); it.value != "main" || it.note != "current" {
		t.Fatalf("the base picker starts at %+v, want the current base main", it)
	}
	e.typeText("par")
	e.pressRun("enter")
	if got := e.m.data.change.BaseBranch; got != "parent" {
		t.Errorf("base after B = %q, want parent; status %q", got, e.m.status)
	}
	if !strings.Contains(e.screen(), "feature onto parent") {
		t.Errorf("the overview does not show the new base:\n%s", e.screen())
	}
}

func TestFuzzyScore(t *testing.T) {
	type result struct {
		score int
		ok    bool
	}
	for _, tc := range []struct {
		value, pattern string
		want           result
	}{
		{"pr/chancez/token_api", "", result{0, true}},
		{"pr/chancez/token_api", "token", result{11, true}},
		{"pr/chancez/token_api", "tkapi", result{1000 + 8, true}},
		{"pr/chancez/token_api", "TOKEN", result{0, false}},
		{"Main", "M", result{0, true}},
		{"main", "xyz", result{0, false}},
	} {
		s, ok := fuzzyScore(tc.value, tc.pattern)
		if got := (result{s, ok}); got != tc.want {
			t.Errorf("fuzzyScore(%q, %q) = %+v, want %+v", tc.value, tc.pattern, got, tc.want)
		}
	}
}

// The picker works by mouse: a click on an entry picks it.
func TestBasePickerByMouse(t *testing.T) {
	e := newTUIEnv(t)
	e.repo.Git("branch", "parent", "main")
	e.pressRun("B")
	y := -1
	for i, line := range strings.Split(e.screen(), "\n") {
		if strings.Contains(line, "| parent") {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("parent not in the picker:\n%s", e.screen())
	}
	_, cmd := e.m.Update(tea.MouseClickMsg{X: e.m.picker.x + 3, Y: y, Button: tea.MouseLeft})
	e.drain(cmd)
	if got := e.m.data.change.BaseBranch; got != "parent" {
		t.Errorf("base after clicking parent = %q, want parent", got)
	}
}
