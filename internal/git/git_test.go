package git_test

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
)

func open(t *testing.T, dir string) *git.Repo {
	t.Helper()
	repo, err := git.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestRootCommit(t *testing.T) {
	r := gittest.New(t)
	repo := open(t, r.Dir)
	if _, err := repo.RootCommit(t.Context()); !errors.Is(err, git.ErrNoCommits) {
		t.Fatalf("RootCommit on empty repo = %v, want ErrNoCommits", err)
	}

	root := r.Commit("root")
	r.Commit("second")
	got, err := repo.RootCommit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("RootCommit = %s, want %s", got, root)
	}

	// An orphan history merged in later is a second, newer root. The oldest one wins.
	r.Git("checkout", "-q", "--orphan", "other")
	r.Commit("other root")
	r.Git("checkout", "-q", "main")
	r.Git("merge", "-q", "--allow-unrelated-histories", "--no-edit", "other")
	got, err = repo.RootCommit(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("RootCommit after merging a second root = %s, want %s", got, root)
	}
}

func TestRootCommitShallow(t *testing.T) {
	src := gittest.New(t)
	src.Commit("one")
	src.Commit("two")
	clone := gittest.New(t)
	clone.Git("fetch", "-q", "--depth=1", "file://"+src.Dir, "main:shallow")
	clone.Git("checkout", "-q", "shallow")
	if _, err := open(t, clone.Dir).RootCommit(t.Context()); !errors.Is(err, git.ErrShallow) {
		t.Fatalf("RootCommit on shallow clone = %v, want ErrShallow", err)
	}
}

func TestCommits(t *testing.T) {
	r := gittest.New(t)
	base := r.Commit("base")
	one := r.Commit("one\n\nbody line")
	two := r.Commit("two")
	repo := open(t, r.Dir)

	got, err := repo.Commits(t.Context(), base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	want := []git.Commit{
		{SHA: one, Parents: []string{base}, AuthorName: "Test", AuthorEmail: "test@example.com", AuthorTime: gittest.CommitTime(1).Local(), Message: "one\n\nbody line\n"},
		{SHA: two, Parents: []string{one}, AuthorName: "Test", AuthorEmail: "test@example.com", AuthorTime: gittest.CommitTime(2).Local(), Message: "two\n"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Commits mismatch (-want +got):\n%s", diff)
	}
}

func TestBranches(t *testing.T) {
	r := gittest.New(t)
	base := r.Commit("base")
	r.Git("checkout", "-q", "-b", "feature")
	head := r.Commit("feature")
	repo := open(t, r.Dir)
	ctx := t.Context()

	branch, err := repo.CurrentBranch(ctx)
	if err != nil || branch != "feature" {
		t.Errorf("CurrentBranch = %q, %v; want feature", branch, err)
	}
	def, err := repo.DefaultBranch(ctx)
	if err != nil || def != "main" {
		t.Errorf("DefaultBranch = %q, %v; want main", def, err)
	}
	for name, want := range map[string]bool{"main": true, "feature": true, "nope": false} {
		got, err := repo.BranchExists(ctx, name)
		if err != nil || got != want {
			t.Errorf("BranchExists(%q) = %v, %v; want %v", name, got, err, want)
		}
	}
	mb, err := repo.MergeBase(ctx, "main", "feature")
	if err != nil || mb != base {
		t.Errorf("MergeBase = %q, %v; want %s", mb, err, base)
	}
	resolved, err := repo.ResolveCommit(ctx, "feature")
	if err != nil || resolved != head {
		t.Errorf("ResolveCommit = %q, %v; want %s", resolved, err, head)
	}
	if ok, err := repo.IsAncestor(ctx, base, head); err != nil || !ok {
		t.Errorf("IsAncestor(base, head) = %v, %v; want true", ok, err)
	}
	if ok, err := repo.IsAncestor(ctx, head, base); err != nil || ok {
		t.Errorf("IsAncestor(head, base) = %v, %v; want false", ok, err)
	}
}

func TestCommitDiff(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", gittest.Lines("one", "two"))
	root := r.Commit("root")
	r.Write("a.txt", gittest.Lines("one", "TWO"))
	second := r.Commit("second")
	repo := open(t, r.Dir)
	ctx := t.Context()

	rootCommit, err := repo.ReadCommit(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.CommitDiff(ctx, rootCommit)
	if err != nil {
		t.Fatal(err)
	}
	rootBlob := r.Git("rev-parse", root+":a.txt")
	secondBlob := r.Git("rev-parse", second+":a.txt")
	want := []git.FileDiff{{
		NewPath: "a.txt", Status: git.StatusAdded, NewMode: "100644", NewBlob: rootBlob,
		Hunks: []git.Hunk{{
			OldStart: 0, OldLines: 0, NewStart: 1, NewLines: 2,
			Lines: []git.Line{
				{Kind: git.LineAdded, Text: "one", NewLine: 1},
				{Kind: git.LineAdded, Text: "two", NewLine: 2},
			},
		}},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("root CommitDiff mismatch (-want +got):\n%s", diff)
	}

	secondCommit, err := repo.ReadCommit(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	got, err = repo.CommitDiff(ctx, secondCommit)
	if err != nil {
		t.Fatal(err)
	}
	want = []git.FileDiff{{
		OldPath: "a.txt", NewPath: "a.txt", Status: git.StatusModified, OldMode: "100644", NewMode: "100644",
		OldBlob: rootBlob, NewBlob: secondBlob,
		Hunks: []git.Hunk{{
			OldStart: 1, OldLines: 2, NewStart: 1, NewLines: 2,
			Lines: []git.Line{
				{Kind: git.LineContext, Text: "one", OldLine: 1, NewLine: 1},
				{Kind: git.LineDeleted, Text: "two", OldLine: 2},
				{Kind: git.LineAdded, Text: "TWO", NewLine: 2},
			},
		}},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("CommitDiff mismatch (-want +got):\n%s", diff)
	}

	content, ok, err := repo.ReadFile(ctx, root, "a.txt")
	if err != nil || !ok || string(content) != "one\ntwo\n" {
		t.Errorf("ReadFile = %q, %v, %v", content, ok, err)
	}
	if _, ok, err := repo.ReadFile(ctx, root, "missing"); err != nil || ok {
		t.Errorf("ReadFile(missing) ok = %v, err = %v; want false, nil", ok, err)
	}
	if id, err := repo.BlobID(ctx, root, "missing"); err != nil || id != "" {
		t.Errorf("BlobID(missing) = %q, %v; want empty", id, err)
	}
}
