package git

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// Worktree is one of a repository's working trees.
type Worktree struct {
	// Dir is its top level.
	Dir string
	// Head is the commit checked out there, "" before the first commit.
	Head string
	// Branch is the branch checked out there, without refs/heads/, "" when detached.
	Branch string
}

// Worktrees lists the repository's working trees, the main one first, then the others by path. Bare ones, and ones whose
// directory is gone, are left out.
func (r *Repo) Worktrees(ctx context.Context) ([]Worktree, error) {
	out, err := r.run(ctx, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var trees []Worktree
	var wt Worktree
	skip := false
	for field := range bytes.SplitSeq(out, []byte{0}) {
		key, value, _ := strings.Cut(string(field), " ")
		switch key {
		case "worktree":
			wt, skip = Worktree{Dir: value}, false
		case "HEAD":
			wt.Head = nonZero(value)
		case "branch":
			wt.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "bare", "prunable":
			skip = true
		case "":
			// A blank field ends the entry.
			if wt.Dir != "" && !skip {
				trees = append(trees, wt)
			}
			wt = Worktree{}
		}
	}
	return trees, nil
}

// Status is git status in the worktree, untracked files listed one by one, as one string that
// changes when a file is added, staged, or unstaged, though not when a changed file changes again.
// It takes no lock, so it never races a git command run meanwhile.
func (r *Repo) Status(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "--no-optional-locks", "status", "--porcelain=v2", "-z", "--untracked-files=all",
		"--ignore-submodules=dirty")
	return string(out), err
}

// Snapshot writes the worktree's files as a tree, staged or not and untracked ones included,
// ignored ones not, and returns it with the commit checked out. The files are added to a copy of
// the index, so the index, and what git status says, are left alone.
func (r *Repo) Snapshot(ctx context.Context) (head, tree string, err error) {
	if head, err = r.line(ctx, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil && exitCode(err) != 1 {
		return "", "", err
	}
	index, err := r.line(ctx, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return "", "", err
	}
	tmp, err := os.CreateTemp("", "gr-index-")
	if err != nil {
		return "", "", err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	// Copied, its stat data says which files are unchanged, so only changed ones are read.
	switch content, err := os.ReadFile(index); {
	case errors.Is(err, fs.ErrNotExist):
		// No index yet: git starts one where the copy would have been, which must not exist.
		os.Remove(tmp.Name())
	case err != nil:
		return "", "", err
	default:
		if err := os.WriteFile(tmp.Name(), content, 0o600); err != nil {
			return "", "", err
		}
	}
	env := []string{"GIT_INDEX_FILE=" + tmp.Name()}
	if _, err := runEnv(ctx, r.Dir, env, nil, "add", "--all"); err != nil {
		return "", "", err
	}
	out, err := runEnv(ctx, r.Dir, env, nil, "write-tree")
	if err != nil {
		return "", "", err
	}
	return head, strings.TrimSpace(string(out)), nil
}
