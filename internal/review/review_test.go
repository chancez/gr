package review_test

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

var (
	human = store.Author{Name: "Human", Email: "human@example.com"}
	agent = store.Author{Name: "agent"}
)

type env struct {
	t     *testing.T
	repo  *gittest.Repo
	store *store.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "gr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return &env{t: t, repo: gittest.New(t), store: st}
}

func (e *env) service(author store.Author) *review.Service {
	e.t.Helper()
	g, err := git.Open(e.t.Context(), e.repo.Dir)
	if err != nil {
		e.t.Fatal(err)
	}
	s, err := review.Open(e.t.Context(), e.store, g, author)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *env) ensure(s *review.Service, spec string) (store.Change, store.Revision) {
	e.t.Helper()
	target, err := s.ParseTarget(e.t.Context(), spec, "")
	if err != nil {
		e.t.Fatal(err)
	}
	c, rev, err := s.EnsureChange(e.t.Context(), target)
	if err != nil {
		e.t.Fatal(err)
	}
	return c, rev
}

// ignoreGenerated drops fields that are generated or time-dependent, so tests can compare the rest.
var ignoreGenerated = cmp.Options{
	cmpopts.IgnoreFields(store.Thread{}, "ID", "ChangeID", "RevisionID", "ReviewID", "CreatedAt", "UpdatedAt", "StateAt"),
	cmpopts.IgnoreFields(store.Location{}, "RevisionID", "BlobSHA"),
	cmpopts.IgnoreFields(store.Comment{}, "ID", "ThreadID", "ReviewID", "CreatedAt", "UpdatedAt"),
}

func TestParseTarget(t *testing.T) {
	e := newEnv(t)
	root := e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	head := e.repo.Commit("feature")
	s := e.service(human)
	ctx := t.Context()

	tests := []struct {
		spec string
		want review.Target
	}{
		{"", review.Target{Kind: store.KindBranch, Branch: "feature", BaseBranch: "main"}},
		{"feature", review.Target{Kind: store.KindBranch, Branch: "feature", BaseBranch: "main"}},
		{"main..feature", review.Target{Kind: store.KindRange, Base: root, Head: head}},
		{head, review.Target{Kind: store.KindCommit, Base: root, Head: head}},
		{root, review.Target{Kind: store.KindCommit, Head: root}},
	}
	for _, tt := range tests {
		got, err := s.ParseTarget(ctx, tt.spec, "")
		if err != nil {
			t.Fatalf("ParseTarget(%q): %v", tt.spec, err)
		}
		if diff := cmp.Diff(tt.want, got); diff != "" {
			t.Errorf("ParseTarget(%q) mismatch (-want +got):\n%s", tt.spec, diff)
		}
	}
	for _, spec := range []string{"main", "main...feature", "nope"} {
		if _, err := s.ParseTarget(ctx, spec, ""); err == nil {
			t.Errorf("ParseTarget(%q) succeeded, want error", spec)
		}
	}
}

func TestRevisions(t *testing.T) {
	e := newEnv(t)
	base := e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	one := e.repo.Commit("one")
	s := e.service(human)
	ctx := t.Context()

	change, rev := e.ensure(s, "")
	if rev.Seq != 1 || rev.BaseSHA != base || rev.HeadSHA != one {
		t.Fatalf("first revision = %+v", rev)
	}
	again, rev2 := e.ensure(s, "")
	if again.ID != change.ID || rev2.ID != rev.ID {
		t.Errorf("ensuring again made a new change or revision: %+v %+v", again, rev2)
	}

	two := e.repo.Commit("two")
	_, rev3, err := s.Sync(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	if rev3.Seq != 2 || rev3.BaseSHA != base || rev3.HeadSHA != two {
		t.Errorf("revision after a new commit = %+v", rev3)
	}

	// Fast-forwarding main onto the branch merges the change.
	e.repo.Git("checkout", "-q", "main")
	e.repo.Git("merge", "-q", "--ff-only", "feature")
	merged, latest, err := s.Sync(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	if merged.State != store.ChangeMerged || latest.ID != rev3.ID {
		t.Errorf("after merging: state %s, revision %d; want merged, 2", merged.State, latest.Seq)
	}
	// The branch can be reviewed again as a new change.
	e.repo.Git("checkout", "-q", "feature")
	e.repo.Commit("three")
	next, _ := e.ensure(s, "")
	if next.ID == change.ID || next.Seq != 2 {
		t.Errorf("new work on a merged branch reused change %d", next.Seq)
	}
}

func TestThreadsAndDrafts(t *testing.T) {
	e := newEnv(t)
	e.repo.Write("a.go", gittest.Lines(gittest.Numbered(10)...))
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	lines := gittest.Numbered(10)
	lines[4] = "changed 5"
	e.repo.Write("a.go", gittest.Lines(lines...))
	commit := e.repo.Commit("change line 5\n\nWith a body.")
	h := e.service(human)
	a := e.service(agent)
	ctx := t.Context()
	change, _ := e.ensure(h, "")

	lineThread, err := h.StartThread(ctx, change, review.NewThread{
		Scope: store.ScopeCommit, Commit: commit, Path: "a.go", StartLine: 5, Body: "why?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.StartThread(ctx, change, review.NewThread{
		Scope: store.ScopeCommit, Commit: commit, Path: review.CommitMsgPath, StartLine: 3, Body: "typo",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.StartThread(ctx, change, review.NewThread{Scope: store.ScopeChange, Body: "overall"}); err != nil {
		t.Fatal(err)
	}
	// Line 1 is unchanged and outside the hunk's context.
	if _, err := h.StartThread(ctx, change, review.NewThread{
		Scope: store.ScopeDiff, Path: "a.go", StartLine: 1, Body: "no",
	}); err == nil {
		t.Error("thread on a line outside the diff was created")
	}

	// The agent sees nothing until the review is submitted.
	if got, err := a.Threads(ctx, change, review.ThreadFilter{}); err != nil || len(got) != 0 {
		t.Fatalf("agent sees %d threads before submit, err %v", len(got), err)
	}
	if _, err := h.Submit(ctx, change, store.VerdictRequestChanges, "please fix"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Reply(ctx, change, lineThread.ID, "done", false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetThreadState(ctx, lineThread.ID, store.ThreadAddressed); err != nil {
		t.Fatal(err)
	}

	got, err := a.Threads(ctx, change, review.ThreadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []review.ThreadView{
		{
			Thread: store.Thread{
				Seq: 1, State: store.ThreadAddressed, StateBy: agent,
				Location: store.Location{
					Scope: store.ScopeCommit, Commit: commit,
					Anchor: anchor.Anchor{
						Path: "a.go", Side: anchor.SideNew, StartLine: 5, EndLine: 5, Lines: []string{"changed 5"},
						Before: []string{"line 2", "line 3", "line 4"}, After: []string{"line 6", "line 7", "line 8"},
					},
				},
			},
			Comments: []store.Comment{{Author: human, Body: "why?"}, {Author: agent, Body: "done"}},
		},
		{
			Thread: store.Thread{
				Seq: 2, State: store.ThreadOpen,
				Location: store.Location{
					Scope: store.ScopeCommit, Commit: commit,
					Anchor: anchor.Anchor{
						Path: review.CommitMsgPath, Side: anchor.SideNew, StartLine: 3, EndLine: 3, Lines: []string{"With a body."},
						Before: []string{"change line 5", ""},
					},
				},
			},
			Comments: []store.Comment{{Author: human, Body: "typo"}},
		},
		{
			Thread:   store.Thread{Seq: 3, State: store.ThreadOpen, Location: store.Location{Scope: store.ScopeChange}},
			Comments: []store.Comment{{Author: human, Body: "overall"}},
		},
	}
	if diff := cmp.Diff(want, got, ignoreGenerated); diff != "" {
		t.Errorf("Threads mismatch (-want +got):\n%s", diff)
	}

	byCommit, err := a.CommitThreads(ctx, commit, review.ThreadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want[:2], byCommit, ignoreGenerated); diff != "" {
		t.Errorf("CommitThreads mismatch (-want +got):\n%s", diff)
	}

	reviews, err := a.Reviews(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	wantReviews := []store.Review{{Seq: 1, Author: human, State: store.ReviewSubmitted, Verdict: store.VerdictRequestChanges, Body: "please fix"}}
	if diff := cmp.Diff(wantReviews, reviews, cmpopts.IgnoreFields(store.Review{}, "ID", "ChangeID", "RevisionID", "CreatedAt", "UpdatedAt", "SubmittedAt")); diff != "" {
		t.Errorf("Reviews mismatch (-want +got):\n%s", diff)
	}
}

func TestReanchorAfterRebase(t *testing.T) {
	e := newEnv(t)
	e.repo.Write("a.go", gittest.Lines(gittest.Numbered(20)...), "b.go", gittest.Lines("b1", "b2", "b3"))
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")

	lines := gittest.Numbered(20)
	lines[9] = "feature 10"
	e.repo.Write("a.go", gittest.Lines(lines...))
	first := e.repo.Commit("first")
	e.repo.Write("b.go", gittest.Lines("b1", "feature b2", "b3"))
	second := e.repo.Commit("second")

	h := e.service(human)
	ctx := t.Context()
	change, _ := e.ensure(h, "")
	start := func(nt review.NewThread) {
		t.Helper()
		if _, err := h.StartThread(ctx, change, nt); err != nil {
			t.Fatal(err)
		}
	}
	start(review.NewThread{Scope: store.ScopeCommit, Commit: first, Path: "a.go", StartLine: 10, Body: "moves"})
	start(review.NewThread{Scope: store.ScopeCommit, Commit: second, Path: "b.go", StartLine: 2, Body: "rewritten away"})
	start(review.NewThread{Scope: store.ScopeCommit, Commit: second, Path: review.CommitMsgPath, StartLine: 1, Body: "subject"})
	start(review.NewThread{Scope: store.ScopeCommit, Commit: second, Body: "whole commit"})
	start(review.NewThread{Scope: store.ScopeDiff, Path: "a.go", StartLine: 10, Body: "diff"})
	if _, err := h.Submit(ctx, change, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}

	// main gains lines at the top of a.go, then the branch is rebased onto it and its second commit
	// is amended so b2 reads differently.
	e.repo.Git("checkout", "-q", "main")
	e.repo.Write("a.go", gittest.Lines(append([]string{"top 1", "top 2"}, gittest.Numbered(20)...)...))
	e.repo.Commit("main moves")
	e.repo.Git("checkout", "-q", "feature")
	e.repo.Git("rebase", "-q", "main")
	e.repo.Write("b.go", gittest.Lines("b1", "other b2", "b3"))
	e.repo.Git("add", "-A")
	e.repo.Git("commit", "-q", "--amend", "--no-edit")
	newFirst := e.repo.Git("rev-parse", "HEAD~1")
	newSecond := e.repo.Git("rev-parse", "HEAD")

	_, rev, err := h.Sync(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Seq != 2 {
		t.Fatalf("revision after rebase = %d, want 2", rev.Seq)
	}
	got, err := h.Threads(ctx, change, review.ThreadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	type summary struct {
		Body      string
		Scope     store.Scope
		Commit    string
		Path      string
		StartLine int
		Outdated  bool
	}
	var summaries []summary
	for _, v := range got {
		summaries = append(summaries, summary{v.Comments[0].Body, v.Location.Scope, v.Location.Commit, v.Location.Path, v.Location.StartLine, v.Outdated})
	}
	want := []summary{
		{"moves", store.ScopeCommit, newFirst, "a.go", 12, false},
		{"rewritten away", store.ScopeCommit, second, "b.go", 2, true},
		{"subject", store.ScopeCommit, newSecond, review.CommitMsgPath, 1, false},
		{"whole commit", store.ScopeCommit, newSecond, "", 0, false},
		{"diff", store.ScopeDiff, "", "a.go", 12, false},
	}
	if diff := cmp.Diff(want, summaries); diff != "" {
		t.Errorf("threads after rebase mismatch (-want +got):\n%s", diff)
	}
}

func TestReanchorToLaterCommit(t *testing.T) {
	e := newEnv(t)
	e.repo.Write("a.go", gittest.Lines(gittest.Numbered(5)...))
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	lines := gittest.Numbered(5)
	lines[2] = "feature 3"
	e.repo.Write("a.go", gittest.Lines(lines...))
	first := e.repo.Commit("first")
	e.repo.Write("c.go", "c\n")
	e.repo.Commit("second")
	h := e.service(human)
	ctx := t.Context()
	change, _ := e.ensure(h, "")
	if _, err := h.StartThread(ctx, change, review.NewThread{Scope: store.ScopeCommit, Commit: first, Path: "a.go", StartLine: 3, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.StartThread(ctx, change, review.NewThread{Scope: store.ScopeDiff, Path: "a.go", StartLine: 3, Body: "y"}); err != nil {
		t.Fatal(err)
	}

	// Drop the first commit and fold its change into an unrelated one.
	e.repo.Git("reset", "-q", "--hard", "main")
	e.repo.Write("c.go", "c\n")
	e.repo.Commit("unrelated")
	e.repo.Write("a.go", gittest.Lines(lines...))
	combined := e.repo.Commit("combined")
	if _, _, err := h.Sync(ctx, change); err != nil {
		t.Fatal(err)
	}
	got, err := h.Threads(ctx, change, review.ThreadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	want := []store.Location{
		{Scope: store.ScopeCommit, Commit: combined, Anchor: anchor.Anchor{
			Path: "a.go", Side: anchor.SideNew, StartLine: 3, EndLine: 3, Lines: []string{"feature 3"},
			Before: []string{"line 1", "line 2"}, After: []string{"line 4", "line 5"},
		}},
		{Scope: store.ScopeDiff, Anchor: anchor.Anchor{
			Path: "a.go", Side: anchor.SideNew, StartLine: 3, EndLine: 3, Lines: []string{"feature 3"},
			Before: []string{"line 1", "line 2"}, After: []string{"line 4", "line 5"},
		}},
	}
	var locs []store.Location
	for _, v := range got {
		if v.Outdated {
			t.Errorf("thread %d is outdated", v.Seq)
		}
		locs = append(locs, v.Location)
	}
	if diff := cmp.Diff(want, locs, ignoreGenerated); diff != "" {
		t.Errorf("locations after rewrite mismatch (-want +got):\n%s", diff)
	}
}

func TestInterdiffExcludesBase(t *testing.T) {
	e := newEnv(t)
	e.repo.Write("a.go", gittest.Lines("a1", "a2"), "m.go", "m1\n")
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Write("a.go", gittest.Lines("a1", "feature a2"))
	e.repo.Commit("feature")
	h := e.service(human)
	ctx := t.Context()
	change, rev1 := e.ensure(h, "")

	e.repo.Git("checkout", "-q", "main")
	e.repo.Write("m.go", "main m1\n")
	e.repo.Commit("main moves")
	e.repo.Git("checkout", "-q", "feature")
	e.repo.Git("rebase", "-q", "main")
	e.repo.Write("a.go", gittest.Lines("a1", "feature a2", "a3"))
	e.repo.Commit("more")
	_, rev2, err := h.Sync(ctx, change)
	if err != nil {
		t.Fatal(err)
	}

	files, exact, err := h.Interdiff(ctx, rev1, rev2)
	if err != nil {
		t.Fatal(err)
	}
	if !exact {
		t.Error("interdiff was not exact")
	}
	want := []git.FileDiff{{
		OldPath: "a.go", NewPath: "a.go", Status: git.StatusModified, OldMode: "100644", NewMode: "100644",
		Hunks: []git.Hunk{{
			OldStart: 1, OldLines: 2, NewStart: 1, NewLines: 3,
			Lines: []git.Line{
				{Kind: git.LineContext, Text: "a1", OldLine: 1, NewLine: 1},
				{Kind: git.LineContext, Text: "feature a2", OldLine: 2, NewLine: 2},
				{Kind: git.LineAdded, Text: "a3", NewLine: 3},
			},
		}},
	}}
	if diff := cmp.Diff(want, files); diff != "" {
		t.Errorf("Interdiff mismatch (-want +got):\n%s", diff)
	}
}

func TestDeleteDraftThread(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	h := e.service(human)
	ctx := t.Context()
	change, _ := e.ensure(h, "")
	v, err := h.StartThread(ctx, change, review.NewThread{Scope: store.ScopeChange, Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.DeleteComment(ctx, v.Comments[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err := h.Threads(ctx, change, review.ThreadFilter{})
	if err != nil || len(got) != 0 {
		t.Errorf("threads after deleting the only comment = %d, %v", len(got), err)
	}
}

func TestParseAuthor(t *testing.T) {
	for in, want := range map[string]store.Author{
		"claude":                  {Name: "claude"},
		"Jo Doe <jo@example.com>": {Name: "Jo Doe", Email: "jo@example.com"},
		" spaced ":                {Name: "spaced"},
	} {
		if got := review.ParseAuthor(in); got != want {
			t.Errorf("ParseAuthor(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParseTargetUsesOpenChangeBase(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	// No main or master, so there is no default branch to fall back on.
	e.repo.Git("branch", "-m", "main", "trunk")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	s := e.service(human)
	ctx := t.Context()

	if _, err := s.ParseTarget(ctx, "", ""); err == nil {
		t.Fatal("ParseTarget with no default branch and no change succeeded")
	}
	target, err := s.ParseTarget(ctx, "", "trunk")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.EnsureChange(ctx, target); err != nil {
		t.Fatal(err)
	}
	got, err := s.ParseTarget(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := review.Target{Kind: store.KindBranch, Branch: "feature", BaseBranch: "trunk"}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ParseTarget mismatch (-want +got):\n%s", diff)
	}
}
