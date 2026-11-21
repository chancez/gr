package review

import (
	"context"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
)

// Uncommitted is what the worktree a branch change is checked out in has that its commit does not:
// files staged, unstaged, and untracked, ignored ones aside. It is shown, not reviewed.
type Uncommitted struct {
	// Dir is the worktree.
	Dir string
	// Status is git's status there, to tell cheaply when it changes.
	Status string
	// Head is the commit checked out there, and Tree its files as they are.
	Head string
	Tree string
	// Files is the diff from Head to Tree, without hunks until loaded.
	Files []git.FileDiff
}

// worktree is the worktree a branch change's branch is checked out in, if any.
func (s *Service) worktree(ctx context.Context, change store.Change) (*git.Repo, bool, error) {
	if change.Kind != store.KindBranch {
		return nil, false, nil
	}
	trees, err := s.git.Worktrees(ctx)
	if err != nil {
		return nil, false, err
	}
	for _, wt := range trees {
		if wt.Branch == change.Branch {
			return &git.Repo{Dir: wt.Dir}, true, nil
		}
	}
	return nil, false, nil
}

// UncommittedStatus is git's status in the worktree a branch change is checked out in, "" when it
// is checked out nowhere or has nothing uncommitted.
func (s *Service) UncommittedStatus(ctx context.Context, change store.Change) (string, error) {
	wt, ok, err := s.worktree(ctx, change)
	if !ok || err != nil {
		return "", err
	}
	return wt.Status(ctx)
}

// Uncommitted reads what a branch change has uncommitted, with its file list. It is empty when the
// branch is checked out nowhere or nothing there is uncommitted.
func (s *Service) Uncommitted(ctx context.Context, change store.Change) (Uncommitted, error) {
	wt, ok, err := s.worktree(ctx, change)
	if !ok || err != nil {
		return Uncommitted{}, err
	}
	u := Uncommitted{Dir: wt.Dir}
	if u.Status, err = wt.Status(ctx); err != nil || u.Status == "" {
		return u, err
	}
	if u.Head, u.Tree, err = wt.Snapshot(ctx); err != nil {
		return u, err
	}
	// The worktrees share their objects, so the tree is read from here.
	u.Files, err = s.diffs.DiffFiles(ctx, u.Head, u.Tree)
	return u, err
}

// WithUncommitted is a revision's whole diff with what is uncommitted on top, with opts.
func (s *Service) WithUncommitted(ctx context.Context, rev store.Revision, u Uncommitted, opts git.DiffOptions) ([]git.FileDiff, error) {
	return s.diff(ctx, rev.BaseSHA, u.Tree, opts)
}

// UncommittedFiles is the diff of what is uncommitted, with opts.
func (s *Service) UncommittedFiles(ctx context.Context, u Uncommitted, opts git.DiffOptions) ([]git.FileDiff, error) {
	return s.diff(ctx, u.Head, u.Tree, opts)
}
