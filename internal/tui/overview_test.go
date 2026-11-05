package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/store"
)

func TestMergeMoves(t *testing.T) {
	base := func(from, to string) store.Move {
		return store.Move{Kind: store.MoveBase, From: from, To: to, How: store.MovedByHand}
	}
	branch := func(from, to string, how store.MoveHow) store.Move {
		return store.Move{Kind: store.MoveBranch, From: from, To: to, How: how}
	}
	var got []string
	for _, m := range mergeMoves([]store.Move{
		base("main", "parent"),
		branch("poc", "wip", store.MovedRenamed),
		base("parent", "other"),
		branch("wip", "feature", store.MovedRenamed),
		base("other", "main"),
		branch("feature", "final", store.MovedByHand),
	}) {
		got = append(got, m.label())
	}
	// The base went back where it started; the branch moved by hand once, so it was not only renamed.
	if diff := cmp.Diff([]string{"branch moved: poc -> final (3 moves)"}, got); diff != "" {
		t.Errorf("merged (-want +got):\n%s", diff)
	}

	got = nil
	for _, m := range mergeMoves([]store.Move{base("main", "parent"), branch("poc", "feature", store.MovedRenamed)}) {
		got = append(got, m.label())
	}
	if diff := cmp.Diff([]string{"base changed: main -> parent", "branch renamed: poc -> feature"}, got); diff != "" {
		t.Errorf("one of each (-want +got):\n%s", diff)
	}
}

// Moves show in the overview between the reviews they came between, merged where no review
// separates them.
func TestOverviewShowsMoves(t *testing.T) {
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
	if _, _, err := e.svc.SetBranch(ctx, c, "feat", store.MovedRenamed); err != nil {
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
