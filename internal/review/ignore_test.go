package review_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
)

func TestIgnored(t *testing.T) {
	e := newEnv(t)
	e.repo.Write("README", "hi\n")
	e.repo.Commit("init")
	// A linked worktree has its own .grignore and shares info/grignore with the main one.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.repo.Git("worktree", "add", "-q", "-b", "topic", filepath.Join(dir, "wt"))
	e.repo.Dir = filepath.Join(dir, "wt")
	global := filepath.Join(t.TempDir(), "ignore")
	for path, lines := range map[string]string{
		global: "*.lock\n",
		filepath.Join(e.repo.Dir, review.IgnoreFile):                                                             "vendor/*\n",
		filepath.Join(e.repo.Git("rev-parse", "--path-format=absolute", "--git-common-dir"), "info", "grignore"): "!vendor/mine.go\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	g, err := git.Open(t.Context(), e.repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s, err := review.Open(t.Context(), e.store, g, human)
	if err != nil {
		t.Fatal(err)
	}
	s.SetGlobalIgnore(global)
	m, err := s.Ignored(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range []string{"go.lock", "vendor/x.go", "vendor/mine.go", "main.go"} {
		if m.Match(p) {
			got = append(got, p)
		}
	}
	if diff := cmp.Diff([]string{"go.lock", "vendor/x.go"}, got); diff != "" {
		t.Errorf("ignored (-want +got):\n%s", diff)
	}
}
