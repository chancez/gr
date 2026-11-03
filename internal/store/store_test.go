package store_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/store"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func open(t *testing.T) *store.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gr.db")
	s, err := store.Open(t.Context(), path, store.WithClock(func() time.Time { return t0 }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ignoreIDs ignores generated ids, which are checked by looking rows back up.
var ignoreIDs = cmpopts.IgnoreFields(store.Thread{}, "ID")

var (
	alice = store.Author{Name: "Alice", Email: "alice@example.com"}
	agent = store.Author{Name: "agent"}
)

type fixture struct {
	repo     store.Repo
	change   store.Change
	revision store.Revision
}

func setup(t *testing.T, s *store.Store) fixture {
	t.Helper()
	ctx := t.Context()
	var f fixture
	err := s.Tx(ctx, func(tx *store.Tx) error {
		var err error
		if f.repo, err = tx.UpsertRepo(ctx, "root", "/src/repo"); err != nil {
			return err
		}
		if f.change, err = tx.CreateChange(ctx, store.Change{RepoID: f.repo.ID, Kind: store.KindBranch, Branch: "feature", BaseBranch: "main"}); err != nil {
			return err
		}
		f.revision, err = tx.CreateRevision(ctx, f.change.ID, "base1", "head1")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRepoUpsert(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	first, err := s.UpsertRepo(ctx, "root", "/a")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := s.UpsertRepo(ctx, "root", "/b")
	if err != nil {
		t.Fatal(err)
	}
	want := store.Repo{ID: first.ID, RootSHA: "root", LastPath: "/b", CreatedAt: t0, UpdatedAt: t0}
	if diff := cmp.Diff(want, moved); diff != "" {
		t.Errorf("UpsertRepo after move mismatch (-want +got):\n%s", diff)
	}
	got, err := s.RepoByRoot(ctx, "root")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("RepoByRoot mismatch (-want +got):\n%s", diff)
	}
	if _, err := s.RepoByRoot(ctx, "other"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("RepoByRoot(other) = %v, want ErrNotFound", err)
	}
}

func TestChanges(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	f := setup(t, s)

	want := store.Change{
		ID: f.change.ID, RepoID: f.repo.ID, Seq: 1, Kind: store.KindBranch, Branch: "feature", BaseBranch: "main",
		State: store.ChangeOpen, CreatedAt: t0, UpdatedAt: t0,
	}
	for name, lookup := range map[string]func() (store.Change, error){
		"GetChange":        func() (store.Change, error) { return s.GetChange(ctx, f.change.ID) },
		"ChangeBySeq":      func() (store.Change, error) { return s.ChangeBySeq(ctx, f.repo.ID, 1) },
		"OpenBranchChange": func() (store.Change, error) { return s.OpenBranchChange(ctx, f.repo.ID, "feature") },
		"Prefix":           func() (store.Change, error) { return s.ResolveChangePrefix(ctx, f.repo.ID, f.change.ID[:8]) },
	} {
		got, err := lookup()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("%s mismatch (-want +got):\n%s", name, diff)
		}
	}

	// A second open change for the same branch is refused, and allowed once the first is merged.
	if _, err := s.CreateChange(ctx, store.Change{RepoID: f.repo.ID, Kind: store.KindBranch, Branch: "feature"}); err == nil {
		t.Error("second open change for a branch was created")
	}
	if err := s.SetChangeState(ctx, f.change.ID, store.ChangeMerged); err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateChange(ctx, store.Change{RepoID: f.repo.ID, Kind: store.KindBranch, Branch: "feature"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Seq != 2 {
		t.Errorf("second change seq = %d, want 2", second.Seq)
	}

	open, err := s.ListChanges(ctx, f.repo.ID, store.ChangeOpen)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]store.Change{second}, open); diff != "" {
		t.Errorf("ListChanges(open) mismatch (-want +got):\n%s", diff)
	}
}

func TestFrozenChange(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	f := setup(t, s)
	c, err := s.CreateChange(ctx, store.Change{RepoID: f.repo.ID, Kind: store.KindRange})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRevision(ctx, c.ID, "a", "b"); err != nil {
		t.Fatal(err)
	}
	got, err := s.FrozenChange(ctx, f.repo.ID, store.KindRange, "a", "b")
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(c, got); diff != "" {
		t.Errorf("FrozenChange mismatch (-want +got):\n%s", diff)
	}
	if _, err := s.FrozenChange(ctx, f.repo.ID, store.KindCommit, "a", "b"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("FrozenChange with other kind = %v, want ErrNotFound", err)
	}
}

func TestRevisions(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	f := setup(t, s)
	second, err := s.CreateRevision(ctx, f.change.ID, "base1", "head2")
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.LatestRevision(ctx, f.change.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := store.Revision{ID: second.ID, ChangeID: f.change.ID, Seq: 2, BaseSHA: "base1", HeadSHA: "head2", CreatedAt: t0}
	if diff := cmp.Diff(want, latest); diff != "" {
		t.Errorf("LatestRevision mismatch (-want +got):\n%s", diff)
	}
	all, err := s.ListRevisions(ctx, f.change.ID)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]store.Revision{f.revision, want}, all); diff != "" {
		t.Errorf("ListRevisions mismatch (-want +got):\n%s", diff)
	}
}

func TestReviewsAndDrafts(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	f := setup(t, s)

	draft, err := s.CreateReview(ctx, f.change.ID, f.revision.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateReview(ctx, f.change.ID, f.revision.ID, alice); err == nil {
		t.Error("second draft review for one author was created")
	}
	got, err := s.DraftReview(ctx, f.change.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(draft, got); diff != "" {
		t.Errorf("DraftReview mismatch (-want +got):\n%s", diff)
	}

	thread, err := s.CreateThread(ctx, store.Thread{
		ChangeID: f.change.ID, RevisionID: f.revision.ID, ReviewID: draft.ID,
		Location: store.Location{
			Scope: store.ScopeCommit, RevisionID: f.revision.ID, Commit: "c1", BlobSHA: "blob",
			Anchor: anchor.Anchor{Path: "a.go", Side: anchor.SideNew, StartLine: 2, EndLine: 3, Lines: []string{"x", "y"}, After: []string{"z"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	comment, err := s.AddComment(ctx, store.Comment{ThreadID: thread.ID, ReviewID: draft.ID, Author: alice, Body: "why?"})
	if err != nil {
		t.Fatal(err)
	}

	// Drafts are visible to their author only.
	for _, tt := range []struct {
		viewer  store.Author
		threads int
	}{{alice, 1}, {agent, 0}} {
		threads, err := s.ListThreads(ctx, store.ThreadFilter{ChangeID: f.change.ID, Viewer: tt.viewer})
		if err != nil {
			t.Fatal(err)
		}
		if len(threads) != tt.threads {
			t.Errorf("ListThreads as %s = %d threads, want %d", tt.viewer, len(threads), tt.threads)
		}
		reviews, err := s.ListReviews(ctx, f.change.ID, tt.viewer)
		if err != nil {
			t.Fatal(err)
		}
		if len(reviews) != tt.threads {
			t.Errorf("ListReviews as %s = %d reviews, want %d", tt.viewer, len(reviews), tt.threads)
		}
	}

	if err := s.SubmitReview(ctx, draft.ID, f.revision.ID, store.VerdictRequestChanges, "see comments"); err != nil {
		t.Fatal(err)
	}
	if err := s.SubmitReview(ctx, draft.ID, f.revision.ID, store.VerdictApprove, ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("resubmitting = %v, want ErrNotFound", err)
	}

	threads, err := s.ListThreads(ctx, store.ThreadFilter{Commit: "c1", Viewer: agent})
	if err != nil {
		t.Fatal(err)
	}
	wantThread := store.Thread{
		ChangeID: f.change.ID, Seq: 1, RevisionID: f.revision.ID, ReviewID: draft.ID, State: store.ThreadOpen,
		Location: store.Location{
			Scope: store.ScopeCommit, RevisionID: f.revision.ID, Commit: "c1", BlobSHA: "blob",
			Anchor: anchor.Anchor{Path: "a.go", Side: anchor.SideNew, StartLine: 2, EndLine: 3, Lines: []string{"x", "y"}, After: []string{"z"}},
		},
		CreatedAt: t0, UpdatedAt: t0,
	}
	if diff := cmp.Diff([]store.Thread{wantThread}, threads, ignoreIDs); diff != "" {
		t.Errorf("ListThreads after submit mismatch (-want +got):\n%s", diff)
	}

	reply, err := s.AddComment(ctx, store.Comment{ThreadID: thread.ID, Author: agent, Body: "fixed"})
	if err != nil {
		t.Fatal(err)
	}
	comments, err := s.ListComments(ctx, thread.ID, agent)
	if err != nil {
		t.Fatal(err)
	}
	wantComments := []store.Comment{
		{ID: comment.ID, ThreadID: thread.ID, ReviewID: draft.ID, Author: alice, Body: "why?", CreatedAt: t0, UpdatedAt: t0},
		{ID: reply.ID, ThreadID: thread.ID, Author: agent, Body: "fixed", CreatedAt: t0, UpdatedAt: t0},
	}
	if diff := cmp.Diff(wantComments, comments); diff != "" {
		t.Errorf("ListComments mismatch (-want +got):\n%s", diff)
	}
}

func TestThreadStateAndMove(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	f := setup(t, s)
	review, err := s.CreateReview(ctx, f.change.ID, f.revision.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.CreateThread(ctx, store.Thread{
		ChangeID: f.change.ID, RevisionID: f.revision.ID, ReviewID: review.ID,
		Location: store.Location{Scope: store.ScopeChange, RevisionID: f.revision.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateRevision(ctx, f.change.ID, "base1", "head2")
	if err != nil {
		t.Fatal(err)
	}
	moved := store.Location{
		Scope: store.ScopeDiff, RevisionID: second.ID,
		Anchor: anchor.Anchor{Path: "b.go", Side: anchor.SideOld, StartLine: 1, EndLine: 1, Lines: []string{"q"}},
	}
	if err := s.MoveThread(ctx, thread.ID, moved, true, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SetThreadState(ctx, thread.ID, store.ThreadAddressed, agent); err != nil {
		t.Fatal(err)
	}
	got, err := s.ThreadBySeq(ctx, f.change.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := store.Thread{
		ID: thread.ID, ChangeID: f.change.ID, Seq: 1, RevisionID: f.revision.ID, ReviewID: review.ID,
		State: store.ThreadAddressed, StateBy: agent, StateAt: t0, Outdated: true, Location: moved,
		CreatedAt: t0, UpdatedAt: t0,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("thread mismatch (-want +got):\n%s", diff)
	}

	byPrefix, err := s.ResolveThreadPrefix(ctx, f.change.ID, thread.ID[:8])
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, byPrefix); diff != "" {
		t.Errorf("ResolveThreadPrefix mismatch (-want +got):\n%s", diff)
	}

	outdated := true
	listed, err := s.ListThreads(ctx, store.ThreadFilter{
		ChangeID: f.change.ID, States: []store.ThreadState{store.ThreadAddressed}, Outdated: &outdated, Viewer: alice,
	})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]store.Thread{want}, listed); diff != "" {
		t.Errorf("ListThreads mismatch (-want +got):\n%s", diff)
	}

	// Discarding the draft review takes its threads with it.
	if err := s.DeleteReview(ctx, review.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetThread(ctx, thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetThread after discarding its review = %v, want ErrNotFound", err)
	}
}

func TestReopenDoesNotWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gr.db")
	s, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, err := s.DataVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	before, err := w.Version(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	again, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	after, err := w.Version(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("reopening an up to date database moved data_version from %d to %d", before, after)
	}

	writer, err := store.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.UpsertRepo(t.Context(), "root", "/x"); err != nil {
		t.Fatal(err)
	}
	written, err := w.Version(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if written == after {
		t.Error("a write from another connection did not move data_version")
	}
}

func TestMarks(t *testing.T) {
	s := open(t)
	ctx := t.Context()
	f := setup(t, s)
	marks := []store.Mark{{Commit: "c1"}, {Commit: "c1", Path: "a.go"}, {Path: "a.go", Blob: "b1"}}
	for _, m := range marks {
		if err := s.AddMark(ctx, f.change.ID, alice, m); err != nil {
			t.Fatal(err)
		}
	}
	// Marking twice is harmless, and other authors have their own marks.
	if err := s.AddMark(ctx, f.change.ID, alice, marks[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.AddMark(ctx, f.change.ID, agent, store.Mark{Commit: "c2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteMark(ctx, f.change.ID, alice, marks[1]); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListMarks(ctx, f.change.ID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]store.Mark{marks[0], marks[2]}, got); diff != "" {
		t.Errorf("ListMarks mismatch (-want +got):\n%s", diff)
	}
}
