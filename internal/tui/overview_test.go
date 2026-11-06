package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/store"
)

func TestMergeEvents(t *testing.T) {
	base := func(from, to string) store.Event {
		return store.Event{Kind: store.EventBase, From: from, To: to, How: store.ByHand}
	}
	branch := func(from, to string, how store.EventHow) store.Event {
		return store.Event{Kind: store.EventBranch, From: from, To: to, How: how}
	}
	var got []string
	for _, m := range mergeEvents([]store.Event{
		base("main", "parent"),
		branch("poc", "wip", store.ByRename),
		base("parent", "other"),
		branch("wip", "feature", store.ByRename),
		base("other", "main"),
		branch("feature", "final", store.ByHand),
	}) {
		got = append(got, m.label("main"))
	}
	// The base went back where it started; the branch moved by hand once, so it was not only renamed.
	if diff := cmp.Diff([]string{"branch moved: poc -> final (3 moves)"}, got); diff != "" {
		t.Errorf("merged (-want +got):\n%s", diff)
	}

	got = nil
	for _, m := range mergeEvents([]store.Event{base("main", "parent"), branch("poc", "feature", store.ByRename)}) {
		got = append(got, m.label("main"))
	}
	if diff := cmp.Diff([]string{"base changed: main -> parent", "branch renamed: poc -> feature"}, got); diff != "" {
		t.Errorf("one of each (-want +got):\n%s", diff)
	}

	state := func(from, to store.ChangeState, how store.EventHow) store.Event {
		return store.Event{Kind: store.EventState, From: string(from), To: string(to), How: how}
	}
	labels := func(events ...store.Event) []string {
		var out []string
		for _, m := range mergeEvents(events) {
			out = append(out, m.label("main"))
		}
		return out
	}
	got = nil
	for _, run := range [][]store.Event{
		{state(store.ChangeOpen, store.ChangeMerged, store.Detected)},
		{state(store.ChangeOpen, store.ChangeMerged, store.ByHand)},
		{state(store.ChangeOpen, store.ChangeAbandoned, store.ByHand)},
		{state(store.ChangeMerged, store.ChangeOpen, store.ByHand)},
		// Abandoned and opened again before anyone looked is nothing.
		{state(store.ChangeOpen, store.ChangeAbandoned, store.ByHand), state(store.ChangeAbandoned, store.ChangeOpen, store.ByHand)},
	} {
		got = append(got, labels(run...)...)
	}
	if diff := cmp.Diff([]string{"merged into main", "marked merged", "abandoned", "reopened"}, got); diff != "" {
		t.Errorf("states (-want +got):\n%s", diff)
	}
}

// Moves show in the overview between the reviews they came between, merged where no review
// separates them.
func TestOverviewShowsEvents(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	e.repo.Git("branch", "a", "main")
	e.repo.Git("branch", "b", "main")
	c := e.chg
	var err error
	for _, base := range []string{"a", "main"} {
		if c, _, err = e.svc.SetBase(ctx, c, base); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.Submit(ctx, c, store.VerdictComment, "first"); err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{"a", "b"} {
		if c, _, err = e.svc.SetBase(ctx, c, base); err != nil {
			t.Fatal(err)
		}
	}
	e.repo.Git("branch", "-m", "feature", "feat")
	if _, _, err := e.svc.SetBranch(ctx, c, "feat", store.ByRename); err != nil {
		t.Fatal(err)
	}
	e.drain(e.m.loadCmd())
	e.openNav("Overview")

	var got []string
	for _, l := range strings.Split(e.screen(), "\n") {
		_, rest, _ := strings.Cut(ansi.Strip(l), "|")
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, "---") || strings.Contains(rest, "Review #") {
			got = append(got, strings.TrimRight(rest, "- "))
		}
	}
	want := []string{
		// a and back to main before the review moved nothing.
		"v Review #1  Human  commented  revision 1",
		"--- base changed: main -> b (2 moves)",
		"--- branch renamed: feature -> feat",
	}
	for i := range got {
		if i < len(want) && strings.HasPrefix(got[i], want[i]) {
			got[i] = want[i]
		}
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("overview timeline (-want +got):\n%s", diff)
	}
}

// Merging the branch into its base while the change is open shows it merged, in the timeline too.
func TestMergeSeenWhileOpen(t *testing.T) {
	e := newTUIEnv(t)
	e.repo.Git("checkout", "-q", "main")
	e.repo.Git("merge", "-q", "--ff-only", "feature")
	e.repo.Git("checkout", "-q", "feature")
	_, cmd := e.m.Update(e.m.refreshCmd()())
	if cmd == nil {
		t.Fatal("moving the base read nothing again")
	}
	e.drain(cmd)
	e.openNav("Overview")
	type seen struct {
		State  store.ChangeState
		Status string
		Merged bool
	}
	got := seen{State: e.m.data.change.State, Status: e.m.status, Merged: strings.Contains(e.screen(), "--- merged into main ---")}
	if diff := cmp.Diff(seen{State: store.ChangeMerged, Status: "change #1 merged into main", Merged: true}, got); diff != "" {
		t.Errorf("after merging (-want +got):\n%s", diff)
	}
}
