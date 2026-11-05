package review_test

import (
	"errors"
	"path/filepath"
	"strconv"
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
	// The summary is numbered by its review, outside the thread numbers, and lists first.
	want = append([]review.ThreadView{{
		Thread:   store.Thread{Seq: store.SummarySeq(1), State: store.ThreadOpen, Summary: true, Location: store.Location{Scope: store.ScopeChange}},
		Comments: []store.Comment{{Author: human, Body: "please fix"}},
	}}, want...)
	if diff := cmp.Diff(want, got, ignoreGenerated); diff != "" {
		t.Errorf("Threads mismatch (-want +got):\n%s", diff)
	}

	byCommit, err := a.CommitThreads(ctx, commit, review.ThreadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want[1:3], byCommit, ignoreGenerated); diff != "" {
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
		Was       []string
	}
	var summaries []summary
	for _, v := range got {
		summaries = append(summaries, summary{v.Comments[0].Body, v.Location.Scope, v.Location.Commit, v.Location.Path, v.Location.StartLine,
			v.Outdated, v.Was})
	}
	// The amend rewrote the line the second commit's thread is on: it follows its commit to the
	// line that replaced it, keeping the one it was made on.
	want := []summary{
		{"moves", store.ScopeCommit, newFirst, "a.go", 12, false, nil},
		{"rewritten away", store.ScopeCommit, newSecond, "b.go", 2, false, []string{"feature b2"}},
		{"subject", store.ScopeCommit, newSecond, review.CommitMsgPath, 1, false, nil},
		{"whole commit", store.ScopeCommit, newSecond, "", 0, false, nil},
		{"diff", store.ScopeDiff, "", "a.go", 12, false, nil},
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
		OldBlob: e.repo.Git("rev-parse", rev1.HeadSHA+":a.go"), NewBlob: e.repo.Git("rev-parse", rev2.HeadSHA+":a.go"),
		Added: 1,
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

func TestMarks(t *testing.T) {
	e := newEnv(t)
	e.repo.Write("a.go", "a\n", "b.go", "b\n")
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Write("a.go", "A\n", "b.go", "B\n")
	commit := e.repo.Commit("change both")
	h := e.service(human)
	ctx := t.Context()
	change, rev := e.ensure(h, "")
	c, err := h.Git().ReadCommit(ctx, commit)
	if err != nil {
		t.Fatal(err)
	}
	files, err := h.CommitFiles(ctx, c)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files[:2] {
		if err := h.SetReviewed(ctx, change, review.CommitFileMark(commit, f), true); err != nil {
			t.Fatal(err)
		}
	}
	marks, err := h.Marks(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	if marks.CommitReviewed(commit, files) {
		t.Error("commit reviewed with one file left")
	}
	if err := h.SetReviewed(ctx, change, review.CommitFileMark(commit, files[2]), true); err != nil {
		t.Fatal(err)
	}
	if marks, _ = h.Marks(ctx, change); !marks.CommitReviewed(commit, files) {
		t.Error("commit not reviewed with every file marked")
	}

	// Unmarking the commit clears its files too.
	if err := h.SetCommitReviewed(ctx, change, commit, files, false); err != nil {
		t.Fatal(err)
	}
	if marks, _ = h.Marks(ctx, change); len(marks) != 0 {
		t.Errorf("marks after unmarking the commit = %v", marks)
	}

	// A whole-diff mark lapses when the file changes.
	diffFiles, err := h.RevisionFiles(ctx, rev)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetReviewed(ctx, change, review.DiffFileMark(diffFiles[0]), true); err != nil {
		t.Fatal(err)
	}
	e.repo.Write("a.go", "AA\n")
	e.repo.Commit("again")
	_, rev2, err := h.Sync(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	newFiles, err := h.RevisionFiles(ctx, rev2)
	if err != nil {
		t.Fatal(err)
	}
	if marks, _ = h.Marks(ctx, change); marks[review.DiffFileMark(newFiles[0])] {
		t.Error("whole-diff mark survived a content change")
	}
}

// Unmarking one file of a reviewed commit leaves its other files reviewed.
func TestSetFileReviewedSplitsACommitMark(t *testing.T) {
	e := newEnv(t)
	e.repo.Write("a.go", "a\n", "b.go", "b\n")
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Write("a.go", "A\n", "b.go", "B\n")
	commit := e.repo.Commit("change both")
	h := e.service(human)
	ctx := t.Context()
	change, _ := e.ensure(h, "")
	c, err := h.Git().ReadCommit(ctx, commit)
	if err != nil {
		t.Fatal(err)
	}
	files, err := h.CommitFiles(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.SetCommitReviewed(ctx, change, commit, files, true); err != nil {
		t.Fatal(err)
	}
	if err := h.SetFileReviewed(ctx, change, commit, files, files[1], false); err != nil {
		t.Fatal(err)
	}
	got, err := h.Marks(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	want := review.Marks{
		review.CommitFileMark(commit, files[0]): true,
		review.CommitFileMark(commit, files[2]): true,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("marks mismatch (-want +got):\n%s", diff)
	}
}

func TestSummaryRef(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	h := e.service(human)
	ctx := t.Context()
	change, _ := e.ensure(h, "")
	line, err := h.StartThread(ctx, change, review.NewThread{Scope: store.ScopeChange, Body: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Submit(ctx, change, store.VerdictComment, "summary"); err != nil {
		t.Fatal(err)
	}
	next, err := h.StartThread(ctx, change, review.NewThread{Scope: store.ScopeChange, Body: "y"})
	if err != nil {
		t.Fatal(err)
	}
	// The summary took no thread number.
	if line.Seq != 1 || next.Seq != 2 {
		t.Errorf("thread numbers = %d, %d; want 1, 2", line.Seq, next.Seq)
	}
	got, err := h.ThreadByRef(ctx, change, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Summary || got.Ref() != "r1" || got.Label() != "r1" || got.Comments[0].Body != "summary" {
		t.Errorf("r1 = %+v", got)
	}
	if _, err := h.ThreadByRef(ctx, change, "-1"); err == nil {
		t.Error("a negative number found a thread")
	}
}

func TestSubmitRefusesEmptyReviews(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	h := e.service(human)
	ctx := t.Context()
	change, _ := e.ensure(h, "")

	for _, v := range []store.Verdict{store.VerdictComment, store.VerdictRequestChanges} {
		if _, err := h.Submit(ctx, change, v, ""); !errors.Is(err, review.ErrEmptyReview) {
			t.Errorf("empty %s review: err = %v, want ErrEmptyReview", v, err)
		}
	}
	// The refused submit leaves no review behind.
	if reviews, _ := h.Reviews(ctx, change); len(reviews) > 1 || (len(reviews) == 1 && reviews[0].State != store.ReviewDraft) {
		t.Errorf("reviews after refused submits = %+v", reviews)
	}
	if _, err := h.Submit(ctx, change, store.VerdictComment, "a summary"); err != nil {
		t.Errorf("review with a summary: %v", err)
	}
	if _, err := h.Submit(ctx, change, store.VerdictApprove, ""); err != nil {
		t.Errorf("bare approval: %v", err)
	}
	// A reply in the draft counts as content.
	other := e.service(agent)
	v, err := other.StartThread(ctx, change, review.NewThread{Scope: store.ScopeChange, Body: "q"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Submit(ctx, change, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Reply(ctx, change, v.ID, "answer", true); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Submit(ctx, change, store.VerdictRequestChanges, ""); err != nil {
		t.Errorf("review with only a draft reply: %v", err)
	}
}

func TestDeletes(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	h, a := e.service(human), e.service(agent)
	ctx := t.Context()
	change, _ := e.ensure(h, "")

	mine, err := h.StartThread(ctx, change, review.NewThread{Scope: store.ScopeChange, Body: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	replied, err := h.StartThread(ctx, change, review.NewThread{Scope: store.ScopeChange, Body: "will get a reply"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Submit(ctx, change, store.VerdictComment, "summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Reply(ctx, change, replied.ID, "a reply", false); err != nil {
		t.Fatal(err)
	}

	// Others' threads, and threads others replied to, are refused.
	if err := a.DeleteThread(ctx, mine.ID); err == nil {
		t.Error("deleted someone else's thread")
	}
	if err := h.DeleteThread(ctx, replied.ID); err == nil {
		t.Error("deleted a thread with someone else's reply")
	}
	if err := h.DeleteReview(ctx, change, 1); err == nil {
		t.Error("deleted a review whose thread has someone else's reply")
	}
	// A thread only you wrote in goes, through its first comment.
	if err := h.DeleteComment(ctx, mine.Comments[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ThreadByRef(ctx, change, strconv.Itoa(mine.Seq)); err == nil {
		t.Error("thread survived deleting its first comment")
	}
	// Deleting the summary thread clears the review's summary.
	if err := h.DeleteThread(ctx, mustRef(t, h, change, "r1").ID); err != nil {
		t.Fatal(err)
	}
	reviews, err := h.Reviews(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	if reviews[0].Body != "" {
		t.Errorf("review summary after deleting its thread = %q", reviews[0].Body)
	}

	// A second review only you wrote in can be deleted, threads and all.
	gone, err := h.StartThread(ctx, change, review.NewThread{Scope: store.ScopeChange, Body: "second round"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Submit(ctx, change, store.VerdictRequestChanges, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteReview(ctx, change, 2); err == nil {
		t.Error("deleted someone else's review")
	}
	if err := h.DeleteReview(ctx, change, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := h.ThreadByRef(ctx, change, strconv.Itoa(gone.Seq)); err == nil {
		t.Error("the deleted review's thread survived")
	}
}

func mustRef(t *testing.T, s *review.Service, change store.Change, ref string) review.ThreadView {
	t.Helper()
	v, err := s.ThreadByRef(t.Context(), change, ref)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// A change merged since gr last looked is marked merged when the branch is next opened, and the
// branch's change from then on is a new one, rather than the merged change.
func TestEnsureMovesPastAMergedChange(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	s := e.service(human)
	ctx := t.Context()
	first, _ := e.ensure(s, "")

	e.repo.Git("checkout", "-q", "main")
	e.repo.Git("merge", "-q", "--ff-only", "feature")
	e.repo.Git("checkout", "-q", "feature")
	next, _ := e.ensure(s, "")
	merged, err := e.store.ChangeBySeq(ctx, first.RepoID, first.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if merged.State != store.ChangeMerged || next.Seq != 2 || next.State != store.ChangeOpen {
		t.Errorf("after merging: change 1 %s, opened change %d %s; want change 1 merged and change 2 open",
			merged.State, next.Seq, next.State)
	}
}

// A stacked branch's change moves onto its parent, then back onto main once the parent lands, and
// its threads follow.
func TestSetBase(t *testing.T) {
	e := newEnv(t)
	e.repo.Write("a.txt", "a\n")
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "parent")
	e.repo.Write("p.txt", "p\n")
	parent := e.repo.Commit("parent")
	e.repo.Git("checkout", "-q", "-b", "child")
	e.repo.Write("c.txt", "c\n")
	child := e.repo.Commit("child")
	s := e.service(human)
	ctx := t.Context()
	target, err := s.ParseTarget(ctx, "", "main")
	if err != nil {
		t.Fatal(err)
	}
	change, _, err := s.EnsureChange(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	tv, err := s.StartThread(ctx, change, review.NewThread{Scope: store.ScopeCommit, Commit: child, Path: "c.txt", StartLine: 1, Body: "c?"})
	if err != nil {
		t.Fatal(err)
	}

	moved, rev, err := s.SetBase(ctx, change, "parent")
	if err != nil {
		t.Fatal(err)
	}
	if moved.BaseBranch != "parent" || rev.BaseSHA != parent || rev.HeadSHA != child || rev.Seq != 2 {
		t.Errorf("onto parent: base %s, revision %d %s..%s; want parent, revision 2 %s..%s",
			moved.BaseBranch, rev.Seq, rev.BaseSHA, rev.HeadSHA, parent, child)
	}
	threads, err := s.Threads(ctx, moved, review.ThreadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].ID != tv.ID || threads[0].Outdated {
		t.Errorf("threads after moving the base = %+v, want the thread, current", threads)
	}
	for _, base := range []string{"child", "nope"} {
		if _, _, err := s.SetBase(ctx, moved, base); err == nil {
			t.Errorf("SetBase(%q) succeeded, want an error", base)
		}
	}
	// Moving onto the base it has is no move.
	if _, _, err := s.SetBase(ctx, moved, "parent"); err != nil {
		t.Fatal(err)
	}
	want := []store.Move{{Kind: store.MoveBase, From: "main", To: "parent", How: store.MovedByHand, Author: human}}
	if diff := cmp.Diff(want, moves(t, s, moved), ignoreMoveIDs); diff != "" {
		t.Errorf("moves (-want +got):\n%s", diff)
	}
}

// Deleting a change takes everything on it, but a change with reviews needs force.
func TestDeleteChange(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	s := e.service(human)
	ctx := t.Context()
	empty, _ := e.ensure(s, "")
	if err := s.DeleteChange(ctx, empty, false); err != nil {
		t.Fatalf("deleting a change with no reviews: %v", err)
	}
	reviewed, _ := e.ensure(s, "")
	if _, err := s.StartThread(ctx, reviewed, review.NewThread{Scope: store.ScopeChange, Body: "hm"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctx, reviewed, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	err := s.DeleteChange(ctx, reviewed, false)
	if want := "change #1 has 1 review and 1 thread; force deletes them with it"; err == nil || err.Error() != want {
		t.Errorf("deleting a reviewed change without force: %v, want %q", err, want)
	}
	if err := s.DeleteChange(ctx, reviewed, true); err != nil {
		t.Fatalf("deleting a reviewed change with force: %v", err)
	}
	changes, err := s.ListChanges(ctx)
	if err != nil || len(changes) != 0 {
		t.Errorf("changes after deleting both = %v, %v; want none", changes, err)
	}
}

// A new branch stacked on another is based on it, so only its own commits are reviewed.
func TestStackedBranchIsBasedOnItsParent(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "parent")
	e.repo.Commit("parent")
	e.repo.Git("checkout", "-q", "-b", "child")
	e.repo.Commit("child")
	s := e.service(human)
	got, err := s.ParseTarget(t.Context(), "", "")
	want := review.Target{Kind: store.KindBranch, Branch: "child", BaseBranch: "parent", Stacked: true}
	if err != nil || got != want {
		t.Errorf("ParseTarget = %+v, %v; want %+v", got, err, want)
	}
	// Given a base, gr uses it.
	got, err = s.ParseTarget(t.Context(), "", "main")
	want = review.Target{Kind: store.KindBranch, Branch: "child", BaseBranch: "main"}
	if err != nil || got != want {
		t.Errorf("ParseTarget with a base = %+v, %v; want %+v", got, err, want)
	}
}

// A branch cut from a stale local main, while the remote's main has moved on, is still reviewed
// from where it was cut: the merge base with the newer remote branch is the stale commit.
func TestBranchFromAStaleMain(t *testing.T) {
	e := newEnv(t)
	stale := e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "other")
	newer := e.repo.Commit("landed upstream")
	e.repo.Git("remote", "add", "work", "https://example.com/work.git")
	e.repo.Git("update-ref", "refs/remotes/work/main", newer)
	e.repo.Git("symbolic-ref", "refs/remotes/work/HEAD", "refs/remotes/work/main")
	e.repo.Git("checkout", "-q", "main")
	e.repo.Git("branch", "-q", "-D", "other")
	e.repo.Git("checkout", "-q", "-b", "feature")
	one := e.repo.Commit("one")
	change, rev := e.ensure(e.service(human), "")
	if change.BaseBranch != "work/main" || rev.BaseSHA != stale || rev.HeadSHA != one {
		t.Errorf("change on %s, revision %s..%s; want work/main, %s..%s", change.BaseBranch, rev.BaseSHA, rev.HeadSHA, stale, one)
	}
}

// Moving a change onto its branch's new name drops a change opened there without reviews.
func TestSetBranch(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "poc")
	e.repo.Commit("one")
	s := e.service(human)
	ctx := t.Context()
	change, _ := e.ensure(s, "")
	if _, err := s.StartThread(ctx, change, review.NewThread{Scope: store.ScopeChange, Body: "hm"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Submit(ctx, change, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	e.repo.Git("branch", "-m", "poc", "feature")
	if fresh, _ := e.ensure(s, ""); fresh.ID == change.ID {
		t.Fatal("the renamed branch found the old change before it was moved")
	}

	type listed struct {
		Seq    int
		Branch string
		Title  string
	}
	list := func() []listed {
		changes, err := s.ListChanges(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var out []listed
		for _, c := range changes {
			out = append(out, listed{Seq: c.Seq, Branch: c.Branch, Title: c.Title})
		}
		return out
	}
	moved, _, err := s.SetBranch(ctx, change, "feature", store.MovedByHand)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]listed{{Seq: 1, Branch: "feature", Title: "feature"}}, list()); diff != "" {
		t.Errorf("after moving #1 onto feature (-want +got):\n%s", diff)
	}

	// A change with reviews on the branch stays, and so does the one that would replace it.
	e.repo.Git("checkout", "-q", "-b", "other")
	other, _ := e.ensure(s, "")
	if _, err := s.Submit(ctx, other, store.VerdictComment, "fine"); err != nil {
		t.Fatal(err)
	}
	for _, branch := range []string{"other", "main", "nope"} {
		if _, _, err := s.SetBranch(ctx, moved, branch, store.MovedByHand); err == nil {
			t.Errorf("SetBranch(%q) succeeded, want an error", branch)
		}
	}
	want := []listed{{Seq: 2, Branch: "other", Title: "other"}, {Seq: 1, Branch: "feature", Title: "feature"}}
	if diff := cmp.Diff(want, list()); diff != "" {
		t.Errorf("after refused moves (-want +got):\n%s", diff)
	}
	// Only the move made is recorded.
	wantMoves := []store.Move{{Kind: store.MoveBranch, From: "poc", To: "feature", How: store.MovedByHand, Author: human}}
	if diff := cmp.Diff(wantMoves, moves(t, s, moved), ignoreMoveIDs); diff != "" {
		t.Errorf("moves (-want +got):\n%s", diff)
	}
}

// ignoreMoveIDs drops a move's generated fields.
var ignoreMoveIDs = cmpopts.IgnoreFields(store.Move{}, "ID", "ChangeID", "CreatedAt")

func moves(t *testing.T, s *review.Service, c store.Change) []store.Move {
	t.Helper()
	m, err := s.Moves(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A change follows its branch through git branch -m, and is only reported for git branch -c.
func TestFollowRename(t *testing.T) {
	e := newEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "poc")
	e.repo.Commit("one")
	s := e.service(human)
	ctx := t.Context()
	reviewed := func() store.Change {
		c, _ := e.ensure(s, "")
		if _, err := s.Submit(ctx, c, store.VerdictComment, "noted"); err != nil {
			t.Fatal(err)
		}
		return c
	}
	reviewed()

	type followed struct {
		OK     bool
		Seq    int
		Branch string
		From   string
		Kept   bool
	}
	follow := func(branch string) followed {
		t.Helper()
		r, ok, err := s.FollowRename(ctx, branch)
		if err != nil {
			t.Fatal(err)
		}
		return followed{OK: ok, Seq: r.Change.Seq, Branch: r.Change.Branch, From: r.From, Kept: r.Kept}
	}

	// Renamed twice, with a change opened on the new name meanwhile, which has no reviews.
	e.repo.Git("branch", "-m", "poc", "wip")
	e.repo.Git("branch", "-m", "wip", "feature")
	e.ensure(s, "")
	if diff := cmp.Diff(followed{OK: true, Seq: 1, Branch: "feature", From: "poc"}, follow("feature")); diff != "" {
		t.Errorf("after renames (-want +got):\n%s", diff)
	}
	c, _ := e.ensure(s, "")
	if c.Seq != 1 {
		t.Errorf("feature's change after following = #%d, want #1", c.Seq)
	}
	// Followed once, from the name it had to the one it has.
	want := []store.Move{{Kind: store.MoveBranch, From: "poc", To: "feature", How: store.MovedRenamed, Author: human}}
	if diff := cmp.Diff(want, moves(t, s, c), ignoreMoveIDs); diff != "" {
		t.Errorf("moves (-want +got):\n%s", diff)
	}
	// Its own change has reviews now, so there is nothing to follow.
	if diff := cmp.Diff(followed{}, follow("feature")); diff != "" {
		t.Errorf("a branch with its own reviewed change (-want +got):\n%s", diff)
	}

	// A copy is reported, and its change left on the branch it was copied from.
	e.repo.Git("branch", "-c", "feature", "copy")
	if diff := cmp.Diff(followed{OK: true, Seq: 1, Branch: "feature", From: "feature", Kept: true}, follow("copy")); diff != "" {
		t.Errorf("a copy (-want +got):\n%s", diff)
	}
	e.repo.Git("branch", "fresh")
	if diff := cmp.Diff(followed{}, follow("fresh")); diff != "" {
		t.Errorf("a branch never renamed (-want +got):\n%s", diff)
	}
}
