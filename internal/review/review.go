// Package review is gr's domain logic: changes, revisions, reviews, threads, and re-anchoring. The
// CLI and TUI both go through it.
package review

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// diffs reads diffs and file lists, through a cache.
	diffs Diffs
	// beforeReanchorWrite, if set, runs between re-anchoring's git work and its write transaction.
	beforeReanchorWrite func()
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
	return &Service{store: st, git: g, repo: repo, author: author, diffs: NewCachedDiffs(g, DefaultCacheLines)}, nil
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
	// Stacked is set when BaseBranch was chosen as the branch Branch is stacked on, rather than
	// given or the default branch.
	Stacked bool
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
// base is the base branch for branch targets, defaulting to the open change's base, then the
// repository's default branch.
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
			// An open change already knows its base.
			change, err := s.store.OpenBranchChange(ctx, s.repo.ID, branch)
			switch {
			case err == nil:
				base = change.BaseBranch
			case !errors.Is(err, store.ErrNotFound):
				return Target{}, err
			}
		}
		stacked := false
		if base == "" {
			if base, err = s.git.DefaultBranch(ctx); err != nil {
				return Target{}, err
			}
			// A branch stacked on another, as a pull request on another's branch, is reviewed
			// against its parent, so only its own commits show.
			if base != branch {
				parents, err := s.git.StackParents(ctx, branch, base)
				if err != nil {
					return Target{}, err
				}
				if len(parents) > 0 {
					base, stacked = parents[0], true
				}
			}
		}
		if base == branch {
			return Target{}, fmt.Errorf("branch %s is its own base; pass a different base branch", branch)
		}
		return Target{Kind: store.KindBranch, Branch: branch, BaseBranch: base, Stacked: stacked}, nil
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

// EnsureChange finds the open change for a target or creates it, then syncs it. A branch's open
// change found to be merged is marked so, and a new change takes its place.
func (s *Service) EnsureChange(ctx context.Context, t Target) (store.Change, store.Revision, error) {
	base, head := t.Base, t.Head
	if t.Kind == store.KindBranch {
		var err error
		if base, head, err = s.branchRevision(ctx, t.Branch, t.BaseBranch); err != nil {
			return store.Change{}, store.Revision{}, err
		}
	}
	change, err := s.findChange(ctx, s.store, t, base, head)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return store.Change{}, store.Revision{}, err
	}
	if err != nil {
		// Read the title from git before the write transaction, so other writers do not wait on it.
		title := s.defaultTitle(ctx, t, base, head)
		var created bool
		var revision store.Revision
		err := s.store.Tx(ctx, func(tx *store.Tx) error {
			// Another process may have created it meanwhile.
			var err error
			change, err = s.findChange(ctx, tx, t, base, head)
			if err == nil || !errors.Is(err, store.ErrNotFound) {
				return err
			}
			created = true
			change, err = tx.CreateChange(ctx, store.Change{
				RepoID:     s.repo.ID,
				Kind:       t.Kind,
				Branch:     t.Branch,
				BaseBranch: t.BaseBranch,
				Title:      title,
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
	}
	synced, revision, err := s.Sync(ctx, change)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	if synced.State != store.ChangeOpen {
		// The branch landed since gr last looked: work on it from here is a new change.
		return s.EnsureChange(ctx, t)
	}
	return synced, revision, nil
}

// changeFinder is a store or a transaction.
type changeFinder interface {
	OpenBranchChange(ctx context.Context, repoID, branch string) (store.Change, error)
	FrozenChange(ctx context.Context, repoID string, kind store.ChangeKind, baseSHA, headSHA string) (store.Change, error)
}

// findChange finds the open change for a target resolved to base and head.
func (s *Service) findChange(ctx context.Context, q changeFinder, t Target, base, head string) (store.Change, error) {
	if t.Kind == store.KindBranch {
		return q.OpenBranchChange(ctx, s.repo.ID, t.Branch)
	}
	return q.FrozenChange(ctx, s.repo.ID, t.Kind, base, head)
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
	for range syncAttempts {
		synced, rev, err := s.sync(ctx, change)
		if !errors.Is(err, errStale) {
			return synced, rev, err
		}
		slog.Info("another writer changed threads while re-anchoring, retrying", "change", change.Seq)
	}
	return store.Change{}, store.Revision{}, fmt.Errorf("sync change #%d: %w", change.Seq, errStale)
}

// syncAttempts bounds how often Sync redoes re-anchoring that other writers raced.
const syncAttempts = 3

func (s *Service) sync(ctx context.Context, change store.Change) (store.Change, store.Revision, error) {
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

	// Git runs before the write transaction, so other writers do not wait on it.
	threads, err := s.store.ListThreads(ctx, store.ThreadFilter{ChangeID: change.ID, AllDrafts: true})
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	r, err := s.newReanchorer(ctx, base, head)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	moves, err := r.plan(ctx, threads)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	marks, err := s.store.ListChangeMarks(ctx, change.ID)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	carried, err := r.carryMarks(ctx, marks)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	if s.beforeReanchorWrite != nil {
		s.beforeReanchorWrite()
	}
	var revision store.Revision
	err = s.store.Tx(ctx, func(tx *store.Tx) error {
		current, err := tx.LatestRevision(ctx, change.ID)
		if err != nil {
			return err
		}
		if current.BaseSHA == base && current.HeadSHA == head {
			// Another process recorded this revision first, and re-anchored.
			revision = current
			return nil
		}
		if current.ID != latest.ID {
			return errStale
		}
		if revision, err = tx.CreateRevision(ctx, change.ID, base, head); err != nil {
			return err
		}
		slog.Info("new revision", "change", change.Seq, "revision", revision.Seq, "base", base, "head", head)
		for _, m := range carried {
			if err := tx.AddMark(ctx, change.ID, m.Author, m.Mark); err != nil {
				return err
			}
		}
		if len(carried) > 0 {
			slog.Info("carried reviewed marks to rewritten commits", "revision", revision.Seq, "marks", len(carried))
		}
		return apply(ctx, tx, change.ID, revision, moves)
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
	slog.Info("change merged", "change", change.Seq)
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

// FindChange finds the open change for a target without creating one, and syncs it. A branch
// target needs only its branch.
func (s *Service) FindChange(ctx context.Context, t Target) (store.Change, store.Revision, error) {
	var change store.Change
	var err error
	if t.Kind == store.KindBranch {
		change, err = s.store.OpenBranchChange(ctx, s.repo.ID, t.Branch)
	} else {
		change, err = s.store.FrozenChange(ctx, s.repo.ID, t.Kind, t.Base, t.Head)
	}
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	return s.Sync(ctx, change)
}

// LatestBranchChange finds a branch's newest change in any state.
func (s *Service) LatestBranchChange(ctx context.Context, branch string) (store.Change, store.Revision, error) {
	changes, err := s.store.ListChanges(ctx, s.repo.ID)
	if err != nil {
		return store.Change{}, store.Revision{}, err
	}
	for _, c := range changes {
		if c.Kind == store.KindBranch && c.Branch == branch {
			return s.Sync(ctx, c)
		}
	}
	return store.Change{}, store.Revision{}, store.ErrNotFound
}

// SetBase moves a branch change onto another base, such as a stacked branch's parent, or main once
// the parent lands. The next revision is cut against the new base, and threads re-anchor onto it.
func (s *Service) SetBase(ctx context.Context, change store.Change, base string) (store.Change, store.Revision, error) {
	fail := func(err error) (store.Change, store.Revision, error) { return store.Change{}, store.Revision{}, err }
	if change.Kind != store.KindBranch {
		return fail(fmt.Errorf("change #%d is a %s, not a branch; it has no base branch to move", change.Seq, change.Kind))
	}
	if base == change.Branch {
		return fail(fmt.Errorf("branch %s cannot be its own base", base))
	}
	if _, err := s.git.ResolveCommit(ctx, base); err != nil {
		return fail(fmt.Errorf("%q is not a branch or commit", base))
	}
	if err := s.store.SetChangeBase(ctx, change.ID, base); err != nil {
		return fail(err)
	}
	slog.Info("change base moved", "change", change.Seq, "from", change.BaseBranch, "to", base)
	change.BaseBranch = base
	return s.Sync(ctx, change)
}

// DeleteChange deletes a change and everything on it. A change with reviews, anyone's drafts
// included, is deleted only with force, since its reviews and threads go with it.
func (s *Service) DeleteChange(ctx context.Context, change store.Change, force bool) error {
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		reviews, threads, err := tx.CountChangeContent(ctx, change.ID)
		if err != nil {
			return err
		}
		if reviews > 0 && !force {
			return fmt.Errorf("change #%d has %s and %s; force deletes them with it",
				change.Seq, plural(reviews, "review", "reviews"), plural(threads, "thread", "threads"))
		}
		slog.Info("change deleted", "change", change.Seq, "reviews", reviews, "threads", threads)
		return tx.DeleteChange(ctx, change.ID)
	})
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
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
