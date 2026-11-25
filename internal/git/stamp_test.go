package git_test

import (
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
)

// age dates each of paths that exists an hour back, as files changed long ago.
func age(t *testing.T, paths []string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	for _, path := range paths {
		if err := os.Chtimes(path, old, old); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
}

// A ref's stamp changes when it, the default branch's candidates, or the config do, and not when
// some other branch moves.
func TestRefFilesStamp(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "a\n")
	r.Commit("root")
	r.Git("branch", "feature/x")
	r.Git("branch", "feature/y")
	r.Write("a.txt", "b\n")
	r.Commit("second")
	repo := open(t, r.Dir)
	files, err := repo.RefFiles(t.Context(), []string{"refs/heads/feature/x", "origin/main"})
	if err != nil {
		t.Fatal(err)
	}
	if got := git.Stamp(files); got != "" {
		t.Errorf("stamp of refs just written = %q, want none", got)
	}
	steps := []struct {
		name string
		do   func()
	}{
		{"other branch", func() { r.Git("branch", "-f", "feature/y", "main") }},
		{"branch", func() { r.Git("branch", "-f", "feature/x", "main") }},
		{"default branch", func() { r.Git("update-ref", "refs/heads/main", "feature/y") }},
		{"remote branch", func() { r.Git("update-ref", "refs/remotes/origin/main", "main") }},
		{"config", func() { r.Git("config", "branch.feature/x.merge", "refs/heads/main") }},
		{"packed", func() { r.Git("pack-refs", "--all") }},
	}
	got := map[string]bool{}
	for _, step := range steps {
		// Listed again, as a new remote's directory is only found by listing.
		if files, err = repo.RefFiles(t.Context(), []string{"refs/heads/feature/x", "origin/main"}); err != nil {
			t.Fatal(err)
		}
		age(t, files)
		before := git.Stamp(files)
		step.do()
		got[step.name] = git.Stamp(files) != before
	}
	want := map[string]bool{
		"other branch":   false,
		"branch":         true,
		"default branch": true,
		"remote branch":  true,
		"config":         true,
		"packed":         true,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("stamp changed by (-want +got):\n%s", diff)
	}
}
