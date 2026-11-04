package git_test

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
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
		NewPath: "a.txt", Status: git.StatusAdded, NewMode: "100644", NewBlob: rootBlob, Added: 2,
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
		OldBlob: rootBlob, NewBlob: secondBlob, Added: 1, Deleted: 1,
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

// TestDefaultBranchFromRemotes covers how the base is found from remotes: git's settings first,
// then origin, upstream, the only remote, or the one that is not the push remote.
func TestDefaultBranchFromRemotes(t *testing.T) {
	type remote struct {
		name     string
		branches []string
		// head is the branch the remote's HEAD points at, or "" for none.
		head string
	}
	for name, tc := range map[string]struct {
		remotes []remote
		local   []string
		config  map[string]string
		// behind moves the remotes' branches a commit past the local ones; ahead moves the local
		// ones a commit past the remotes'.
		behind  bool
		ahead   bool
		want    string
		wantErr bool
	}{
		"origin's HEAD, checked out locally": {
			remotes: []remote{{name: "origin", branches: []string{"main"}, head: "main"}},
			local:   []string{"main"},
			want:    "main",
		},
		"the only remote": {
			remotes: []remote{{name: "work", branches: []string{"trunk"}, head: "trunk"}},
			want:    "work/trunk",
		},
		"the only remote, its default checked out": {
			remotes: []remote{{name: "work", branches: []string{"trunk"}, head: "trunk"}},
			local:   []string{"trunk"},
			want:    "trunk",
		},
		"origin before another remote": {
			remotes: []remote{
				{name: "fork", branches: []string{"mine"}, head: "mine"},
				{name: "origin", branches: []string{"main"}, head: "main"},
			},
			want: "origin/main",
		},
		"origin before upstream": {
			remotes: []remote{
				{name: "origin", branches: []string{"fork-main"}, head: "fork-main"},
				{name: "upstream", branches: []string{"main"}, head: "main"},
			},
			want: "origin/fork-main",
		},
		"checkout.defaultRemote before origin": {
			remotes: []remote{
				{name: "origin", branches: []string{"fork-main"}, head: "fork-main"},
				{name: "upstream", branches: []string{"main"}, head: "main"},
			},
			config: map[string]string{"checkout.defaultRemote": "upstream"},
			want:   "upstream/main",
		},
		"clone.defaultRemoteName names the remote": {
			remotes: []remote{
				{name: "a", branches: []string{"a-main"}, head: "a-main"},
				{name: "b", branches: []string{"b-main"}, head: "b-main"},
			},
			config: map[string]string{"clone.defaultRemoteName": "b"},
			want:   "b/b-main",
		},
		"upstream when there is no origin": {
			remotes: []remote{
				{name: "a", branches: []string{"a-main"}, head: "a-main"},
				{name: "upstream", branches: []string{"develop"}, head: "develop"},
			},
			want: "upstream/develop",
		},
		"the remote that is not the push remote": {
			remotes: []remote{
				{name: "fork", branches: []string{"mine"}, head: "mine"},
				{name: "theirs", branches: []string{"main"}, head: "main"},
			},
			config: map[string]string{"remote.pushDefault": "fork"},
			want:   "theirs/main",
		},
		"a remote without a recorded HEAD": {
			remotes: []remote{{name: "work", branches: []string{"main"}}},
			want:    "work/main",
		},
		"init.defaultBranch on a remote without a HEAD": {
			remotes: []remote{{name: "work", branches: []string{"main", "trunk"}}},
			config:  map[string]string{"init.defaultBranch": "trunk"},
			want:    "work/trunk",
		},
		"no remotes": {
			local: []string{"master"},
			want:  "master",
		},
		"several remotes and nothing to choose by": {
			remotes: []remote{
				{name: "a", branches: []string{"main"}, head: "main"},
				{name: "b", branches: []string{"main"}, head: "main"},
			},
			wantErr: true,
		},
		"several remotes and nothing to choose by, but a local main": {
			remotes: []remote{
				{name: "a", branches: []string{"trunk"}, head: "trunk"},
				{name: "b", branches: []string{"trunk"}, head: "trunk"},
			},
			local: []string{"main"},
			want:  "main",
		},
		"a local main behind the remote's": {
			remotes: []remote{{name: "work", branches: []string{"main"}, head: "main"}},
			local:   []string{"main"},
			behind:  true,
			want:    "work/main",
		},
		"a local main with commits of its own": {
			remotes: []remote{{name: "work", branches: []string{"main"}, head: "main"}},
			local:   []string{"main"},
			ahead:   true,
			want:    "main",
		},
		"a remote with nothing fetched, and a local main": {
			remotes: []remote{{name: "origin"}},
			local:   []string{"main"},
			want:    "main",
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := gittest.New(t)
			sha := r.Commit("base")
			r.Git("checkout", "-q", "-b", "feature")
			r.Git("branch", "-q", "-D", "main")
			for _, b := range tc.local {
				r.Git("branch", "-q", b, sha)
			}
			for _, rem := range tc.remotes {
				r.Git("remote", "add", rem.name, "https://example.com/"+rem.name+".git")
				for _, b := range rem.branches {
					r.Git("update-ref", "refs/remotes/"+rem.name+"/"+b, sha)
				}
				if rem.head != "" {
					r.Git("symbolic-ref", "refs/remotes/"+rem.name+"/HEAD", "refs/remotes/"+rem.name+"/"+rem.head)
				}
			}
			for k, v := range tc.config {
				r.Git("config", k, v)
			}
			if tc.behind || tc.ahead {
				later := r.Git("commit-tree", sha+"^{tree}", "-p", sha, "-m", "later")
				if tc.behind {
					for _, rem := range tc.remotes {
						for _, b := range rem.branches {
							r.Git("update-ref", "refs/remotes/"+rem.name+"/"+b, later)
						}
					}
				}
				if tc.ahead {
					for _, b := range tc.local {
						r.Git("update-ref", "refs/heads/"+b, later)
					}
				}
			}
			got, err := open(t, r.Dir).DefaultBranch(t.Context())
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("DefaultBranch = %q, %v; want %q, error %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

// A branch stacked on another names it as its parent, nearest first; branches already on the base,
// and branches at the same commit, are not parents.
func TestStackParents(t *testing.T) {
	r := gittest.New(t)
	r.Commit("root")
	r.Git("branch", "landed")
	r.Git("checkout", "-q", "-b", "grandparent")
	r.Commit("gp")
	r.Git("checkout", "-q", "-b", "parent")
	r.Commit("p")
	r.Git("checkout", "-q", "-b", "child")
	r.Commit("c")
	r.Git("branch", "copy")
	got, err := open(t, r.Dir).StackParents(t.Context(), "child", "main")
	if want := []string{"parent", "grandparent"}; err != nil || !slices.Equal(got, want) {
		t.Errorf("StackParents = %q, %v; want %q", got, err, want)
	}
	got, err = open(t, r.Dir).StackParents(t.Context(), "grandparent", "main")
	if err != nil || len(got) != 0 {
		t.Errorf("StackParents of a branch on main = %q, %v; want none", got, err)
	}
}

// A branch based on a newer upstream main than the local main has no parents among the branches
// that landed upstream since.
func TestStackParentsIgnoresWhatLandedUpstream(t *testing.T) {
	r := gittest.New(t)
	r.Commit("root")
	r.Git("checkout", "-q", "-b", "landed")
	r.Commit("landed upstream")
	r.Git("remote", "add", "work", "https://example.com/work.git")
	r.Git("update-ref", "refs/remotes/work/main", "landed")
	r.Git("branch", "--set-upstream-to=work/main", "main")
	r.Git("checkout", "-q", "-b", "feature")
	r.Commit("feature")
	got, err := open(t, r.Dir).StackParents(t.Context(), "feature", "main")
	if err != nil || len(got) != 0 {
		t.Errorf("StackParents = %q, %v; want none, since landed is on main's upstream", got, err)
	}
}

// A clean rebase keeps a commit's patch id, and changing its diff does not.
func TestPatchIDs(t *testing.T) {
	r := gittest.New(t)
	repo := open(t, r.Dir)
	r.Write("a.txt", "a\n")
	r.Commit("root")
	r.Git("checkout", "-q", "-b", "topic")
	r.Write("b.txt", "b\n")
	r.Commit("add b")
	before := strings.TrimSpace(r.Git("rev-parse", "HEAD"))
	r.Git("checkout", "-q", "main")
	r.Write("c.txt", "c\n")
	r.Commit("add c")
	r.Git("checkout", "-q", "topic")
	r.Git("rebase", "-q", "main")
	rebased := strings.TrimSpace(r.Git("rev-parse", "HEAD"))
	r.Write("b.txt", "b, fixed\n")
	r.Git("commit", "-q", "--amend", "-a", "--no-edit")
	amended := strings.TrimSpace(r.Git("rev-parse", "HEAD"))

	ids, err := repo.PatchIDs(t.Context(), []string{before, rebased, amended})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[before] == "" || ids[before] != ids[rebased] || ids[amended] == ids[before] {
		t.Errorf("patch ids = %v; want the rebased commit's the same as before, and the amended one's not", ids)
	}
}

// A repository is named for its main worktree, so a linked worktree takes the repository's name.
func TestRepoName(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "a\n")
	r.Commit("root")
	wt := filepath.Join(t.TempDir(), "elsewhere")
	r.Git("worktree", "add", "-q", "-b", "side", wt)
	for _, dir := range []string{r.Dir, wt} {
		if got, err := git.RepoName(t.Context(), dir); err != nil || got != filepath.Base(r.Dir) {
			t.Errorf("RepoName(%s) = %q, %v; want %q", dir, got, err, filepath.Base(r.Dir))
		}
	}
}

func TestMergeTree(t *testing.T) {
	r := gittest.New(t)
	r.Write("a.txt", "base\n", "b.txt", "base\n")
	base := r.Commit("base")
	r.Write("a.txt", "ours\n")
	ours := r.Commit("ours")
	r.Git("checkout", "-q", "-b", "theirs", base)
	r.Write("b.txt", "theirs\n")
	clean := r.Commit("theirs, apart from ours")
	r.Write("a.txt", "theirs\n")
	conflicting := r.Commit("theirs, on ours' line")
	repo := open(t, r.Dir)

	type merged struct {
		Clean bool
		A     string
		B     string
	}
	merge := func(theirs string) merged {
		t.Helper()
		tree, ok, err := repo.MergeTree(t.Context(), base, ours, theirs)
		if err != nil {
			t.Fatal(err)
		}
		return merged{Clean: ok, A: r.Git("show", tree+":a.txt"), B: r.Git("show", tree+":b.txt")}
	}

	if diff := cmp.Diff(merged{Clean: true, A: "ours", B: "theirs"}, merge(clean)); diff != "" {
		t.Errorf("clean merge (-want +got):\n%s", diff)
	}
	// A conflict is a merge too: its tree has the markers, as the interdiff shows them.
	want := merged{
		A: "<<<<<<< " + ours + "\nours\n=======\ntheirs\n>>>>>>> " + conflicting,
		B: "theirs",
	}
	if diff := cmp.Diff(want, merge(conflicting)); diff != "" {
		t.Errorf("conflicting merge (-want +got):\n%s", diff)
	}
}
