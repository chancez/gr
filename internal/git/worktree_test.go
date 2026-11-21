package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
)

func TestWorktrees(t *testing.T) {
	r := gittest.New(t)
	head := r.Commit("root")
	// Resolved, as git reports worktrees by their real paths.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	feature, detached, gone := filepath.Join(dir, "feature"), filepath.Join(dir, "detached"), filepath.Join(dir, "gone")
	r.Git("worktree", "add", "-q", "-b", "feature", feature)
	r.Git("worktree", "add", "-q", "--detach", detached)
	r.Git("worktree", "add", "-q", "-b", "gone", gone)
	// A worktree whose directory was removed without git knowing.
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	got, err := open(t, r.Dir).Worktrees(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []git.Worktree{
		{Dir: r.Dir, Head: head, Branch: "main"},
		{Dir: detached, Head: head},
		{Dir: feature, Head: head, Branch: "feature"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Worktrees (-want +got):\n%s", diff)
	}
}

func TestSnapshot(t *testing.T) {
	r := gittest.New(t)
	r.Write(".gitignore", "ignored\n", "kept", "kept\n", "changed", "old\n", "deleted", "deleted\n")
	head := r.Commit("root")
	r.Write("changed", "new\n", "staged", "staged\n", "untracked", "untracked\n", "ignored", "ignored\n")
	r.Git("add", "staged")
	r.Git("rm", "-q", "--cached", "deleted")
	status := r.Git("status", "--porcelain")

	repo := open(t, r.Dir)
	gotHead, tree, err := repo.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if gotHead != head {
		t.Errorf("head = %s, want %s", gotHead, head)
	}
	if got := r.Git("status", "--porcelain"); got != status {
		t.Errorf("status after Snapshot = %q, want %q", got, status)
	}
	files := map[string]string{}
	for _, path := range strings.Split(r.Git("ls-tree", "-r", "--name-only", tree), "\n") {
		files[path] = r.Git("cat-file", "blob", tree+":"+path)
	}
	want := map[string]string{
		".gitignore": "ignored",
		"kept":       "kept",
		"changed":    "new",
		"staged":     "staged",
		"untracked":  "untracked",
		// Unstaged from the index, but still in the worktree.
		"deleted": "deleted",
	}
	if diff := cmp.Diff(want, files); diff != "" {
		t.Errorf("snapshot (-want +got):\n%s", diff)
	}
}

func TestSnapshotNoCommits(t *testing.T) {
	r := gittest.New(t)
	r.Write("new", "new\n")
	head, tree, err := open(t, r.Dir).Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := []string{head, r.Git("ls-tree", "-r", "--name-only", tree)}
	if diff := cmp.Diff([]string{"", "new"}, got); diff != "" {
		t.Errorf("Snapshot (-want +got):\n%s", diff)
	}
}

func TestStatus(t *testing.T) {
	r := gittest.New(t)
	r.Write("a", "a\n")
	r.Commit("root")
	repo := open(t, r.Dir)
	clean, err := repo.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	r.Write("dir/untracked", "u\n")
	dirty, err := repo.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if clean != "" || !strings.Contains(dirty, "dir/untracked") {
		t.Errorf("Status = %q, then %q; want empty, then naming dir/untracked", clean, dirty)
	}
}
