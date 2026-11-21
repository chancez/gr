package review_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
)

func TestUncommitted(t *testing.T) {
	e := newEnv(t)
	e.repo.Write("a", "a\n")
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Write("a", "a\nb\n")
	head := e.repo.Commit("feature")
	s := e.service(human)
	change, _ := e.ensure(s, "feature")

	// Checked out elsewhere, in a worktree of its own.
	e.repo.Git("checkout", "-q", "main")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.repo.Git("worktree", "add", "-q", dir, "feature")
	clean, err := s.Uncommitted(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(review.Uncommitted{Dir: dir}, clean); diff != "" {
		t.Errorf("Uncommitted when clean (-want +got):\n%s", diff)
	}

	e.repo.Git("-C", dir, "rm", "-q", "a")
	if err := os.WriteFile(filepath.Join(dir, "new"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.Uncommitted(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	want := review.Uncommitted{
		Dir:  dir,
		Head: head,
		Files: []git.FileDiff{
			{OldPath: "a", Status: git.StatusDeleted, Deleted: 2},
			{NewPath: "new", Status: git.StatusAdded, Added: 1},
		},
	}
	opts := cmp.Options{
		cmpopts.IgnoreFields(review.Uncommitted{}, "Status", "Tree"),
		cmpopts.IgnoreFields(git.FileDiff{}, "OldBlob", "NewBlob", "OldMode", "NewMode"),
	}
	if diff := cmp.Diff(want, got, opts); diff != "" {
		t.Errorf("Uncommitted (-want +got):\n%s", diff)
	}
	if got.Status == "" || got.Tree == "" {
		t.Errorf("Uncommitted has status %q and tree %q, want both", got.Status, got.Tree)
	}
	status, err := s.UncommittedStatus(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	if status != got.Status {
		t.Errorf("UncommittedStatus = %q, want %q", status, got.Status)
	}

	// Checked out nowhere, a branch has nothing uncommitted.
	e.repo.Git("-C", dir, "checkout", "-q", "--detach")
	none, err := s.Uncommitted(t.Context(), change)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(review.Uncommitted{}, none); diff != "" {
		t.Errorf("Uncommitted when checked out nowhere (-want +got):\n%s", diff)
	}
}
