package review

import (
	"context"
	"errors"
	"log/slog"
	"reflect"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
)

// reanchorer moves threads onto a new revision. See docs/design.md for the rules.
type reanchorer struct {
	s           *Service
	base, head  string
	commits     []git.Commit
	commitFiles map[string][]git.FileDiff
	rangeFiles  []git.FileDiff
	rangeLoaded bool
}

func (s *Service) newReanchorer(ctx context.Context, base, head string) (*reanchorer, error) {
	commits, err := s.git.Commits(ctx, base, head)
	if err != nil {
		return nil, err
	}
	return &reanchorer{s: s, base: base, head: head, commits: commits, commitFiles: map[string][]git.FileDiff{}}, nil
}

// errStale is returned from a write transaction when threads or revisions changed after the git
// work it writes was done.
var errStale = errors.New("threads changed while re-anchoring")

// threadMove is where re-anchoring puts one thread.
type threadMove struct {
	// from is the location the move was worked out from.
	from     store.Location
	to       store.Location
	outdated bool
}

// plan works out where each thread moves. It runs git but writes nothing, so it runs before the write
// transaction.
func (r *reanchorer) plan(ctx context.Context, threads []store.Thread) (map[string]threadMove, error) {
	moves := make(map[string]threadMove, len(threads))
	for _, t := range threads {
		loc, ok, err := r.locate(ctx, t.Location)
		if err != nil {
			return nil, err
		}
		if !ok {
			// Outdated threads keep their last location for display, and are retried on the next
			// revision.
			loc = t.Location
		}
		moves[t.ID] = threadMove{
			from:     t.Location,
			to:       loc,
			outdated: !ok,
		}
	}
	return moves, nil
}

// apply moves a change's threads onto rev as planned, without running git. It returns errStale if a
// thread is not where the plan found it, or was started since.
func apply(ctx context.Context, tx *store.Tx, changeID string, rev store.Revision, moves map[string]threadMove) error {
	threads, err := tx.ListThreads(ctx, store.ThreadFilter{ChangeID: changeID, AllDrafts: true})
	if err != nil {
		return err
	}
	for _, t := range threads {
		m, ok := moves[t.ID]
		if !ok || !reflect.DeepEqual(m.from, t.Location) {
			return errStale
		}
	}
	outdated := 0
	for _, t := range threads {
		m := moves[t.ID]
		if m.outdated {
			outdated++
		} else {
			m.to.RevisionID = rev.ID
		}
		if err := tx.MoveThread(ctx, t.ID, m.to, m.outdated); err != nil {
			return err
		}
	}
	slog.Info("re-anchored threads", "revision", rev.Seq, "threads", len(threads), "outdated", outdated)
	return nil
}

func (r *reanchorer) files(ctx context.Context, c git.Commit) ([]git.FileDiff, error) {
	if files, ok := r.commitFiles[c.SHA]; ok {
		return files, nil
	}
	files, err := r.s.commitDiff(ctx, c)
	if err != nil {
		return nil, err
	}
	r.commitFiles[c.SHA] = files
	return files, nil
}

func (r *reanchorer) wholeDiff(ctx context.Context) ([]git.FileDiff, error) {
	if r.rangeLoaded {
		return r.rangeFiles, nil
	}
	files, err := r.s.diffs.Diff(ctx, r.base, r.head)
	if err != nil {
		return nil, err
	}
	r.rangeFiles, r.rangeLoaded = files, true
	return files, nil
}

// locate finds where a location belongs in the new revision. The location found has no revision;
// apply sets it.
func (r *reanchorer) locate(ctx context.Context, loc store.Location) (store.Location, bool, error) {
	switch loc.Scope {
	case store.ScopeChange:
		loc.RevisionID = ""
		return loc, true, nil
	case store.ScopeCommit:
		for _, c := range r.commits {
			if c.SHA == loc.Commit {
				loc.RevisionID = ""
				return loc, true, nil
			}
		}
		if loc.Path == "" || loc.Path == CommitMsgPath {
			return r.locateMessage(ctx, loc)
		}
		for _, c := range r.commits {
			files, err := r.files(ctx, c)
			if err != nil {
				return store.Location{}, false, err
			}
			oldRev := ""
			if len(c.Parents) > 0 {
				oldRev = c.Parents[0]
			}
			moved, ok, err := r.match(ctx, loc, files, oldRev, c.SHA)
			if err != nil || ok {
				moved.Scope, moved.Commit, moved.RevisionID = store.ScopeCommit, c.SHA, ""
				return moved, ok, err
			}
		}
		fallthrough
	case store.ScopeDiff:
		if loc.Path == "" || loc.Path == CommitMsgPath {
			return store.Location{}, false, nil
		}
		files, err := r.wholeDiff(ctx)
		if err != nil {
			return store.Location{}, false, err
		}
		moved, ok, err := r.match(ctx, loc, files, r.base, r.head)
		moved.Scope, moved.Commit, moved.RevisionID = store.ScopeDiff, "", ""
		return moved, ok, err
	}
	return store.Location{}, false, nil
}

// match finds a file or line location in a diff from oldRev to newRev.
func (r *reanchorer) match(ctx context.Context, loc store.Location, files []git.FileDiff, oldRev, newRev string) (store.Location, bool, error) {
	if loc.StartLine == 0 {
		if !touches(files, loc.Path) {
			return store.Location{}, false, nil
		}
		blob, err := r.s.blobID(ctx, loc.Side, oldRev, newRev, loc.Path)
		return store.Location{BlobSHA: blob, Anchor: anchor.Anchor{Path: loc.Path, Side: loc.Side}}, true, err
	}
	f, ok := findFile(files, loc.Side, loc.Path)
	if !ok {
		return store.Location{}, false, nil
	}
	rev := newRev
	if loc.Side == anchor.SideOld {
		rev = oldRev
	}
	content, err := r.s.FileLines(ctx, rev, loc.Path)
	if err != nil {
		return store.Location{}, false, err
	}
	start, ok := loc.Find(content)
	if !ok || !anchor.InDiff(f, loc.Side, start, start+len(loc.Lines)-1) {
		return store.Location{}, false, nil
	}
	blob, err := r.s.blobID(ctx, loc.Side, oldRev, newRev, loc.Path)
	return store.Location{BlobSHA: blob, Anchor: loc.Moved(content, start)}, true, err
}

// locateMessage moves a thread on a whole commit or its message to the commit with the same subject.
func (r *reanchorer) locateMessage(ctx context.Context, loc store.Location) (store.Location, bool, error) {
	old, err := r.s.git.ReadCommit(ctx, loc.Commit)
	if err != nil {
		// The old commit has been garbage collected.
		return store.Location{}, false, nil
	}
	for _, c := range r.commits {
		if c.Subject() != old.Subject() {
			continue
		}
		moved := loc
		moved.Commit, moved.RevisionID = c.SHA, ""
		if loc.StartLine == 0 {
			return moved, true, nil
		}
		content := anchor.SplitLines([]byte(c.Message))
		start, ok := loc.Find(content)
		if !ok {
			return store.Location{}, false, nil
		}
		moved.Anchor = loc.Moved(content, start)
		return moved, true, nil
	}
	return store.Location{}, false, nil
}

func (s *Service) blobID(ctx context.Context, side anchor.Side, oldRev, newRev, path string) (string, error) {
	rev := newRev
	if side == anchor.SideOld {
		rev = oldRev
	}
	if rev == "" {
		return "", nil
	}
	return s.git.BlobID(ctx, rev, path)
}
