package review

import (
	"context"
	"errors"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
)

// rewrites finds the commit of a new revision that rewrote one of an earlier revision, by its
// author and author date, as ADR 11 decides.
type rewrites struct {
	s       *Service
	commits []git.Commit
	// byIdentity is the new revision's commits by author email and author date.
	byIdentity map[identity][]git.Commit
	patchIDs   map[string]string
	found      map[string]rewrite
}

// identity is what a rewrite keeps of a commit.
type identity struct {
	email string
	time  int64
}

// rewrite is the commit that rewrote another, and whether its diff is the same.
type rewrite struct {
	commit git.Commit
	same   bool
	ok     bool
}

func newRewrites(s *Service, commits []git.Commit) *rewrites {
	w := &rewrites{s: s, commits: commits, byIdentity: map[identity][]git.Commit{}, patchIDs: map[string]string{},
		found: map[string]rewrite{}}
	for _, c := range commits {
		id := identity{email: c.AuthorEmail, time: c.AuthorTime.Unix()}
		w.byIdentity[id] = append(w.byIdentity[id], c)
	}
	return w
}

// of is the commit of the new revision that is old or rewrote it, and whether its diff is the
// same. A commit no longer in the repository, or with no one commit to match, has none.
func (w *rewrites) of(ctx context.Context, old string) (rewrite, error) {
	if rw, ok := w.found[old]; ok {
		return rw, nil
	}
	rw, err := w.find(ctx, old)
	if err != nil {
		return rewrite{}, err
	}
	w.found[old] = rw
	return rw, nil
}

func (w *rewrites) find(ctx context.Context, old string) (rewrite, error) {
	for _, c := range w.commits {
		if c.SHA == old {
			return rewrite{commit: c, same: true, ok: true}, nil
		}
	}
	prev, err := w.s.git.ReadCommit(ctx, old)
	if _, gone := errors.AsType[*git.Error](err); gone {
		// Pruned since: there is nothing left to match it by.
		return rewrite{}, nil
	}
	if err != nil {
		return rewrite{}, err
	}
	candidates := w.byIdentity[identity{email: prev.AuthorEmail, time: prev.AuthorTime.Unix()}]
	if len(candidates) > 1 {
		var bySubject []git.Commit
		for _, c := range candidates {
			if c.Subject() == prev.Subject() {
				bySubject = append(bySubject, c)
			}
		}
		candidates = bySubject
	}
	if len(candidates) != 1 {
		return rewrite{}, nil
	}
	next := candidates[0]
	ids, err := w.patchIDsOf(ctx, prev.SHA, next.SHA)
	if err != nil {
		return rewrite{}, err
	}
	return rewrite{commit: next, same: ids[prev.SHA] != "" && ids[prev.SHA] == ids[next.SHA], ok: true}, nil
}

// patchIDsOf is the patch ids of shas, worked out once each.
func (w *rewrites) patchIDsOf(ctx context.Context, shas ...string) (map[string]string, error) {
	var missing []string
	for _, sha := range shas {
		if _, ok := w.patchIDs[sha]; !ok {
			missing = append(missing, sha)
		}
	}
	ids, err := w.s.git.PatchIDs(ctx, missing)
	if err != nil {
		return nil, err
	}
	for _, sha := range missing {
		// A commit with no diff has no patch id; it is recorded as empty, so it is not asked again.
		w.patchIDs[sha] = ids[sha]
	}
	return w.patchIDs, nil
}

// carryMarks works out the marks on rewritten commits that move to the commits that rewrote them:
// a commit's mark when its diff is the same, and otherwise the marks of its files whose content is
// the same in both.
func (r *reanchorer) carryMarks(ctx context.Context, marks []store.AuthorMark) ([]store.AuthorMark, error) {
	var out []store.AuthorMark
	for _, am := range marks {
		m := am.Mark
		if m.Commit == "" {
			continue
		}
		rw, err := r.rewrites.of(ctx, m.Commit)
		if err != nil {
			return nil, err
		}
		if !rw.ok || rw.commit.SHA == m.Commit {
			continue
		}
		if rw.same {
			out = append(out, store.AuthorMark{Author: am.Author, Mark: store.Mark{Commit: rw.commit.SHA, Path: m.Path}})
			continue
		}
		same, err := r.unchangedFiles(ctx, m.Commit, rw.commit)
		if err != nil {
			return nil, err
		}
		for _, f := range same {
			if m.Path == "" || m.Path == f.Path() {
				out = append(out, store.AuthorMark{Author: am.Author, Mark: CommitFileMark(rw.commit.SHA, f)})
			}
		}
	}
	return out, nil
}

// unchangedFiles are the files next, which rewrote old, changes as old did: from and to the same
// content, so what was reviewed is what is there.
func (r *reanchorer) unchangedFiles(ctx context.Context, old string, next git.Commit) ([]git.FileDiff, error) {
	prev, err := r.s.git.ReadCommit(ctx, old)
	if err != nil {
		return nil, err
	}
	before, err := r.files(ctx, prev)
	if err != nil {
		return nil, err
	}
	after, err := r.files(ctx, next)
	if err != nil {
		return nil, err
	}
	type change struct {
		old string
		new string
	}
	changes := map[string]change{}
	for _, f := range before {
		changes[f.Path()] = change{old: f.OldBlob, new: f.NewBlob}
	}
	var same []git.FileDiff
	for _, f := range after {
		if c, ok := changes[f.Path()]; ok && c == (change{old: f.OldBlob, new: f.NewBlob}) {
			same = append(same, f)
		}
	}
	return same, nil
}
