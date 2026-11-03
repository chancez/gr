package review_test

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// rebasedChange is a change with a thread on line 3 of a.go, whose branch has since been rebased
// onto a main that gained two lines at the top of a.go. Syncing it records revision 2.
func rebasedChange(t *testing.T) (*env, *review.Service, store.Change) {
	t.Helper()
	e := newEnv(t)
	e.repo.Write("a.go", gittest.Lines(gittest.Numbered(5)...))
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	lines := gittest.Numbered(5)
	lines[2] = "feature 3"
	e.repo.Write("a.go", gittest.Lines(lines...))
	e.repo.Commit("first")
	h := e.service(human)
	change, _ := e.ensure(h, "")
	nt := review.NewThread{
		Scope:     store.ScopeDiff,
		Path:      "a.go",
		StartLine: 3,
		Body:      "x",
	}
	if _, err := h.StartThread(t.Context(), change, nt); err != nil {
		t.Fatal(err)
	}
	e.repo.Git("checkout", "-q", "main")
	e.repo.Write("a.go", gittest.Lines(append([]string{"top 1", "top 2"}, gittest.Numbered(5)...)...))
	e.repo.Commit("main moves")
	e.repo.Git("checkout", "-q", "feature")
	e.repo.Git("rebase", "-q", "main")
	return e, h, change
}

// rebasedLocation is where a thread of rebasedChange belongs in revision 2.
var rebasedLocation = store.Location{
	Scope: store.ScopeDiff,
	Anchor: anchor.Anchor{
		Path:      "a.go",
		Side:      anchor.SideNew,
		StartLine: 5,
		EndLine:   5,
		Lines:     []string{"feature 3"},
		Before:    []string{"top 2", "line 1", "line 2"},
		After:     []string{"line 4", "line 5"},
	},
}

// assertSynced checks the change has two revisions, and its threads are where want says on the
// second.
func assertSynced(t *testing.T, s *review.Service, change store.Change, want []store.Location) {
	t.Helper()
	ctx := t.Context()
	revs, err := s.Revisions(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int
	for _, r := range revs {
		seqs = append(seqs, r.Seq)
	}
	if diff := cmp.Diff([]int{1, 2}, seqs); diff != "" {
		t.Fatalf("revisions mismatch (-want +got):\n%s", diff)
	}
	got, err := s.Store().ListThreads(ctx, store.ThreadFilter{ChangeID: change.ID, AllDrafts: true})
	if err != nil {
		t.Fatal(err)
	}
	var locs []store.Location
	for _, v := range got {
		if v.Outdated {
			t.Errorf("thread %d is outdated", v.Seq)
		}
		if v.Location.RevisionID != revs[1].ID {
			t.Errorf("thread %d is on revision %s, want %s", v.Seq, v.Location.RevisionID, revs[1].ID)
		}
		locs = append(locs, v.Location)
	}
	if diff := cmp.Diff(want, locs, ignoreGenerated); diff != "" {
		t.Errorf("locations mismatch (-want +got):\n%s", diff)
	}
}

func TestReanchorWritesWithoutGit(t *testing.T) {
	_, h, change := rebasedChange(t)
	// Git cannot be found from when the write transaction is about to start.
	path := os.Getenv("PATH")
	t.Setenv("PATH", path)
	h.SetBeforeReanchorWrite(func() { os.Setenv("PATH", t.TempDir()) })
	_, _, err := h.Sync(t.Context(), change)
	os.Setenv("PATH", path)
	if err != nil {
		t.Fatal(err)
	}
	assertSynced(t, h, change, []store.Location{rebasedLocation})
}

func TestReanchorAfterAnotherWriterRecordedTheRevision(t *testing.T) {
	e, h, change := rebasedChange(t)
	a := e.service(agent)
	h.SetBeforeReanchorWrite(func() {
		if _, _, err := a.Sync(t.Context(), change); err != nil {
			t.Fatal(err)
		}
	})
	if _, _, err := h.Sync(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	assertSynced(t, h, change, []store.Location{rebasedLocation})
}

func TestReanchorRedoesForAThreadStartedMeanwhile(t *testing.T) {
	e, h, change := rebasedChange(t)
	a := e.service(agent)
	started := false
	h.SetBeforeReanchorWrite(func() {
		if started {
			return
		}
		started = true
		// The agent still sees revision 1.
		nt := review.NewThread{
			Scope:     store.ScopeDiff,
			Path:      "a.go",
			StartLine: 3,
			Body:      "y",
		}
		if _, err := a.StartThread(t.Context(), change, nt); err != nil {
			t.Fatal(err)
		}
	})
	if _, _, err := h.Sync(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	assertSynced(t, a, change, []store.Location{rebasedLocation, rebasedLocation})
}
