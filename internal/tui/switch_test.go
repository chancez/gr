package tui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
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

// Picking a branch copied from one with a change asks to move that change onto it.
func TestSwitchToCopiedBranch(t *testing.T) {
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
	e.repo.Git("branch", "-c", "other", "copy")
	e.repo.Git("checkout", "-q", "feature")
	e.press("w")
	e.typeText("copy")
	e.pressRun("enter")
	type shown struct {
		Seq    int
		Branch string
		Mode   mode
		Prompt string
	}
	c := e.m.data.change
	want := shown{Seq: 3, Branch: "copy", Mode: modeConfirm,
		Prompt: "copy was copied from other. Move change #2 onto copy, in place of this new change?"}
	if got := (shown{Seq: c.Seq, Branch: c.Branch, Mode: e.m.mode, Prompt: e.m.confirm.prompt}); got != want {
		t.Fatalf("after picking the copy: %+v, want %+v", got, want)
	}

	e.pressRun("y")
	c = e.m.data.change
	want = shown{Seq: 2, Branch: "copy", Mode: modeNormal}
	if got := (shown{Seq: c.Seq, Branch: c.Branch, Mode: e.m.mode}); got != want {
		t.Errorf("after y: %+v, want %+v", got, want)
	}
	changes, err := e.svc.ListChanges(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var branches []string
	for _, c := range changes {
		branches = append(branches, fmt.Sprintf("#%d %s", c.Seq, c.Branch))
	}
	if want := []string{"#2 copy", "#1 feature"}; !reflect.DeepEqual(branches, want) {
		t.Errorf("changes after y = %q, want %q", branches, want)
	}
}

// A tab opened on a copy, as from the command line, asks as it opens.
func TestNewTabOffersTheCopiedChange(t *testing.T) {
	e := newTUIEnv(t)
	r := review.Rename{Change: e.m.data.change, From: "feature", To: "copy", Kept: true}
	m, err := newTab(t.Context(), e.svc, e.m.data.change, Options{Rename: &r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.close)
	if want := "copy was copied from feature. Move change #1 onto copy, in place of this new change?"; m.mode != modeConfirm || m.confirm.prompt != want {
		t.Errorf("new tab: mode %d prompt %q, want the offer %q", m.mode, m.confirm.prompt, want)
	}
}

// A copy opened in a new tab leaves the offer to that tab.
func TestCopiedBranchInNewTab(t *testing.T) {
	e := newTUIEnv(t)
	e.repo.Git("branch", "-c", "feature", "copy")
	msg, ok := e.m.reviewBranch(e.svc, "copy", inNewTab)().(newTabMsg)
	if !ok {
		t.Fatal("opening the copy in a new tab sent no new tab")
	}
	type opened struct {
		Branch string
		Rename review.Rename
		Mode   mode
	}
	got := opened{Branch: msg.change.Branch, Mode: e.m.mode}
	if msg.rename != nil {
		got.Rename = review.Rename{From: msg.rename.From, To: msg.rename.To, Kept: msg.rename.Kept}
	}
	want := opened{Branch: "copy", Rename: review.Rename{From: "feature", To: "copy", Kept: true}, Mode: modeNormal}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("new tab (-want +got):\n%s", diff)
	}
}

// landedChecks runs the background checks among what cmd batches, for what they found.
func landedChecks(cmd tea.Cmd) []landedMsg {
	var out []landedMsg
	for _, c := range cmd().(tea.BatchMsg) {
		if c == nil {
			continue
		}
		if msg, ok := c().(landedMsg); ok {
			out = append(out, msg)
		}
	}
	return out
}

// A change whose branch landed since gr last looked is offered as open until the check in the
// background finds it landed, and then as its branch.
func TestSwitchPickerSeesLanded(t *testing.T) {
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
	e.repo.Git("checkout", "-q", "main")
	e.repo.Git("merge", "-q", "--ff-only", "other")
	e.repo.Git("checkout", "-q", "feature")
	_, cmd := e.m.Update(keyMsg("w"))
	shown := func() []string {
		out := []string{e.m.picker.heading()}
		for _, it := range e.m.picker.items {
			if !it.heading {
				out = append(out, it.value+" ("+it.note+")")
			}
		}
		return out
	}
	want := []string{"Switch to a change  (checking)", "#2 other onto main ()", "#1 feature onto main (current)"}
	if diff := cmp.Diff(want, shown()); diff != "" {
		t.Errorf("as opened (-want +got):\n%s", diff)
	}
	for _, msg := range landedChecks(cmd) {
		e.m.Update(msg)
	}
	want = []string{"Switch to a change", "other (branch, new change)", "#1 feature onto main (current)"}
	if diff := cmp.Diff(want, shown()); diff != "" {
		t.Errorf("after the check (-want +got):\n%s", diff)
	}
}

// Closing the picker cancels its check, which leaves the change it had not got to as it was.
func TestClosingPickerCancelsCheck(t *testing.T) {
	e := newTUIEnv(t)
	e.repo.Git("checkout", "-q", "main")
	e.repo.Git("merge", "-q", "--ff-only", "feature")
	_, cmd := e.m.Update(keyMsg("w"))
	e.press("esc")
	var canceled []bool
	for _, msg := range landedChecks(cmd) {
		canceled = append(canceled, errors.Is(msg.err, context.Canceled))
		e.m.Update(msg)
	}
	if diff := cmp.Diff([]bool{true}, canceled); diff != "" {
		t.Errorf("checks canceled (-want +got):\n%s", diff)
	}
	open, err := e.svc.ListChanges(t.Context(), store.ChangeOpen)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range open {
		ids = append(ids, c.ID)
	}
	if diff := cmp.Diff([]string{e.m.changeID}, ids); diff != "" {
		t.Errorf("open changes (-want +got):\n%s", diff)
	}
}
