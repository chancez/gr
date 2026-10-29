// Package review is gr's domain logic: changes, revisions, reviews, threads, and re-anchoring. The
// CLI and TUI both go through it.
package review

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
)

// CommitMsgPath is the virtual file a commit message is presented as.
const CommitMsgPath = "/COMMIT_MSG"

// Service operates on one repository as one author.
type Service struct {
	store  *store.Store
	git    *git.Repo
	repo   store.Repo
	author store.Author
}

// Open identifies the repository and records where it was last seen.
func Open(ctx context.Context, st *store.Store, g *git.Repo, author store.Author) (*Service, error) {
	if author.Name == "" {
		return nil, errors.New("no author: set git config user.name, GR_AUTHOR, or --author")
	}
	root, err := g.RootCommit(ctx)
	if err != nil {
		return nil, err
	}
	repo, err := st.UpsertRepo(ctx, root, g.Dir)
	if err != nil {
		return nil, err
	}
	return &Service{store: st, git: g, repo: repo, author: author}, nil
}

// Author is who the service acts as.
func (s *Service) Author() store.Author { return s.author }

// Repo is the repository the service operates on.
func (s *Service) Repo() store.Repo { return s.repo }

// Git is the repository's git handle.
func (s *Service) Git() *git.Repo { return s.git }

// Store is the underlying store.
func (s *Service) Store() *store.Store { return s.store }

// ParseAuthor parses "Name <email>" or a bare name.
func ParseAuthor(s string) store.Author {
	s = strings.TrimSpace(s)
	if name, rest, ok := strings.Cut(s, "<"); ok {
		if email, ok := strings.CutSuffix(strings.TrimSpace(rest), ">"); ok {
			return store.Author{Name: strings.TrimSpace(name), Email: strings.TrimSpace(email)}
		}
	}
	return store.Author{Name: s}
}

// Target is what a change reviews.
type Target struct {
	Kind store.ChangeKind
	// Branch and BaseBranch are set for branch targets.
	Branch     string
	BaseBranch string
	// Base and Head are set for range and commit targets. Base is empty for a root commit.
	Base string
	Head string
}

// ParseTarget interprets a target spec:
//
//   - "" is the current branch
//   - "A..B" is a commit range
//   - a local branch name is that branch
//   - anything else that resolves to a commit is that single commit
//
// base is the base branch for branch targets, defaulting to the repository's default branch.
func (s *Service) ParseTarget(ctx context.Context, spec, base string) (Target, error) {
	if strings.Contains(spec, "...") {
		return Target{}, fmt.Errorf("%q: symmetric ranges are not supported, use A..B", spec)
	}
	if from, to, ok := strings.Cut(spec, ".."); ok {
		baseSHA, err := s.git.ResolveCommit(ctx, from)
		if err != nil {
			return Target{}, err
		}
		headSHA, err := s.git.ResolveCommit(ctx, cmp.Or(to, "HEAD"))
		if err != nil {
			return Target{}, err
		}
		return Target{Kind: store.KindRange, Base: baseSHA, Head: headSHA}, nil
	}
	branch := spec
	if branch == "" {
		var err error
		if branch, err = s.git.CurrentBranch(ctx); err != nil {
			return Target{}, fmt.Errorf("HEAD is not on a branch; name a branch, range, or commit: %w", err)
		}
	}
	isBranch, err := s.git.BranchExists(ctx, branch)
	if err != nil {
		return Target{}, err
	}
	if isBranch {
		if base == "" {
			if base, err = s.git.DefaultBranch(ctx); err != nil {
				return Target{}, err
			}
		}
		if base == branch {
			return Target{}, fmt.Errorf("branch %s is its own base; pass a different base branch", branch)
		}
		return Target{Kind: store.KindBranch, Branch: branch, BaseBranch: base}, nil
	}
	sha, err := s.git.ResolveCommit(ctx, spec)
	if err != nil {
		return Target{}, fmt.Errorf("%q is not a branch, range, or commit", spec)
	}
	c, err := s.git.ReadCommit(ctx, sha)
	if err != nil {
		return Target{}, err
	}
	parent := ""
	if len(c.Parents) > 0 {
		parent = c.Parents[0]
	}
	return Target{Kind: store.KindCommit, Base: parent, Head: sha}, nil
}

// branchRevision resolves a branch change's current base and head.
func (s *Service) branchRevision(ctx context.Context, branch, baseBranch string) (base, head string, err error) {
	if head, err = s.git.ResolveCommit(ctx, "refs/heads/"+branch); err != nil {
		return "", "", err
	}
	if base, err = s.git.MergeBase(ctx, baseBranch, head); err != nil {
		return "", "", fmt.Errorf("find merge base of %s and %s: %w", baseBranch, branch, err)
	}
	return base, head, nil
}

// EnsureChange finds the open change for a target or creates it, then syncs it.
func (s *Service) EnsureChange(ctx context.Context, t Target) (store.Change, store.Revision, error) {
	base, head := t.Base, t.Head
	if t.Kind == store.KindBranch {
		var err error
		if base, head, err = s.branchRevision(ctx, t.Branch, t.BaseBranch); err != nil {
			return store.Change{}, store.Revision{}, err
		}
	}
	var change store.Change
	var created bool
	var revision store.Revision
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		var err error
		if t.Kind == store.KindBranch {
			change, err = tx.OpenBranchChange(ctx, s.repo.ID, t.Branch)
		} else {
			change, err = tx.FrozenChange(ctx, s.repo.ID, t.Kind, base, head)
		}
		if err == nil || !errors.Is(err, store.ErrNotFound) {
			return err
		}
		created = true
		change, err = tx.CreateChange(ctx, store.Change{
			RepoID: s.repo.ID, Kind: t.Kind, Branch: t.Branch, BaseBranch: t.BaseBranch,
			Title: s.defaultTitle(ctx, t, base, head),
		})
		if err != nil {
			return err
		}
		revision, err = tx.CreateRevision(ctx, change.ID, base, head)
		return err
	})
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	if created {
		return change, revision, nil
	}
	return s.Sync(ctx, change)
}

func (s *Service) defaultTitle(ctx context.Context, t Target, base, head string) string {
	if t.Kind == store.KindBranch {
		return t.Branch
	}
	if t.Kind == store.KindCommit {
		if c, err := s.git.ReadCommit(ctx, head); err == nil {
			return c.Subject()
		}
	}
	return shortSHA(base) + ".." + shortSHA(head)
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// Sync records a new revision if a branch change's head or base has moved, re-anchoring its threads,
// and marks it merged once its commits have landed on the base branch. It returns the change and its
// latest revision. Nothing is written when nothing moved.
func (s *Service) Sync(ctx context.Context, change store.Change) (store.Change, store.Revision, error) {
	latest, err := s.store.LatestRevision(ctx, change.ID)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	if change.Kind != store.KindBranch || change.State != store.ChangeOpen {
		return change, latest, nil
	}

	exists, err := s.git.BranchExists(ctx, change.Branch)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	if !exists {
		// A branch deleted after landing is merged. Any other deleted branch keeps its last revision.
		merged, err := s.git.IsAncestor(ctx, latest.HeadSHA, change.BaseBranch)
		if err != nil || !merged || latest.BaseSHA == latest.HeadSHA {
			return change, latest, nil
		}
		return s.markMerged(ctx, change, latest)
	}

	base, head, err := s.branchRevision(ctx, change.Branch, change.BaseBranch)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	if base == latest.BaseSHA && head == latest.HeadSHA {
		return change, latest, nil
	}
	if base == head && latest.BaseSHA != latest.HeadSHA {
		// The branch's commits are all on the base branch now.
		return s.markMerged(ctx, change, latest)
	}

	r, err := s.newReanchorer(ctx, base, head)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	var revision store.Revision
	err = s.store.Tx(ctx, func(tx *store.Tx) error {
		// Another process may have recorded this revision first.
		current, err := tx.LatestRevision(ctx, change.ID)
		if err != nil {
			return err
		}
		if current.BaseSHA == base && current.HeadSHA == head {
			revision = current
			return nil
		}
		if revision, err = tx.CreateRevision(ctx, change.ID, base, head); err != nil {
			return err
		}
		return r.reanchorAll(ctx, tx, change.ID, revision)
	})
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	return change, revision, nil
}

func (s *Service) markMerged(ctx context.Context, change store.Change, latest store.Revision) (store.Change, store.Revision, error) {
	if err := s.store.SetChangeState(ctx, change.ID, store.ChangeMerged); err != nil {
		return store.Change{}, store.Revision{}, err
	}
	change.State = store.ChangeMerged
	return change, latest, nil
}

// ChangeByRef finds a change in this repository by number or id prefix, and syncs it.
func (s *Service) ChangeByRef(ctx context.Context, ref string) (store.Change, store.Revision, error) {
	ref = strings.TrimPrefix(ref, "#")
	var change store.Change
	var err error
	if seq, convErr := strconv.Atoi(ref); convErr == nil {
		change, err = s.store.ChangeBySeq(ctx, s.repo.ID, seq)
	} else {
		change, err = s.store.ResolveChangePrefix(ctx, s.repo.ID, ref)
	}
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	return s.Sync(ctx, change)
}

// ListChanges lists this repository's changes, newest first.
func (s *Service) ListChanges(ctx context.Context, states ...store.ChangeState) ([]store.Change, error) {
	return s.store.ListChanges(ctx, s.repo.ID, states...)
}

// SetChangeState abandons or reopens a change.
func (s *Service) SetChangeState(ctx context.Context, change store.Change, state store.ChangeState) error {
	return s.store.SetChangeState(ctx, change.ID, state)
}

// Revisions lists a change's revisions, oldest first.
func (s *Service) Revisions(ctx context.Context, change store.Change) ([]store.Revision, error) {
	return s.store.ListRevisions(ctx, change.ID)
}

// RevisionBySeq finds a revision of a change by number.
func (s *Service) RevisionBySeq(ctx context.Context, change store.Change, seq int) (store.Revision, error) {
	return s.store.RevisionBySeq(ctx, change.ID, seq)
}
