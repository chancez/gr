package review

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
)

// NewThread describes a thread to start.
type NewThread struct {
	Scope store.Scope
	// Commit is a commit-ish in the change, for ScopeCommit.
	Commit string
	// Path is a file in the commit or diff, or CommitMsgPath. Empty means the whole commit or change.
	Path string
	// Side defaults to new.
	Side anchor.Side
	// StartLine and EndLine are 1-based and inclusive. Zero means the whole file. EndLine defaults to
	// StartLine.
	StartLine int
	EndLine   int
	Body      string
}

// ThreadView is a thread with the comments the viewer can see.
type ThreadView struct {
	store.Thread
	Comments []store.Comment
}

// StartThread starts a thread in the author's draft review on the change's latest revision.
func (s *Service) StartThread(ctx context.Context, change store.Change, nt NewThread) (ThreadView, error) {
	if strings.TrimSpace(nt.Body) == "" {
		return ThreadView{}, errors.New("comment body is empty")
	}
	rev, err := s.store.LatestRevision(ctx, change.ID)
	if err != nil {
		return ThreadView{}, err
	}
	loc, err := s.locationFor(ctx, rev, nt)
	if err != nil {
		return ThreadView{}, err
	}
	var view ThreadView
	err = s.store.Tx(ctx, func(tx *store.Tx) error {
		review, err := s.draft(ctx, tx, change, rev)
		if err != nil {
			return err
		}
		t, err := tx.CreateThread(ctx, store.Thread{ChangeID: change.ID, RevisionID: rev.ID, ReviewID: review.ID, Location: loc})
		if err != nil {
			return err
		}
		c, err := tx.AddComment(ctx, store.Comment{ThreadID: t.ID, ReviewID: review.ID, Author: s.author, Body: nt.Body})
		if err != nil {
			return err
		}
		view = ThreadView{Thread: t, Comments: []store.Comment{c}}
		return nil
	})
	return view, err
}

// draft returns the author's draft review, creating it if needed.
func (s *Service) draft(ctx context.Context, tx *store.Tx, change store.Change, rev store.Revision) (store.Review, error) {
	review, err := tx.DraftReview(ctx, change.ID, s.author)
	if errors.Is(err, store.ErrNotFound) {
		return tx.CreateReview(ctx, change.ID, rev.ID, s.author)
	}
	return review, err
}

// locationFor validates a new thread's target against a revision and captures its anchor.
func (s *Service) locationFor(ctx context.Context, rev store.Revision, nt NewThread) (store.Location, error) {
	side := cmp.Or(nt.Side, anchor.SideNew)
	if side != anchor.SideNew && side != anchor.SideOld {
		return store.Location{}, fmt.Errorf("side must be old or new, not %q", side)
	}
	if nt.EndLine == 0 {
		nt.EndLine = nt.StartLine
	}
	if nt.StartLine == 0 && nt.EndLine != 0 {
		return store.Location{}, errors.New("an end line needs a start line")
	}
	if nt.StartLine != 0 && nt.Path == "" {
		return store.Location{}, errors.New("lines need a path")
	}
	loc := store.Location{Scope: nt.Scope, RevisionID: rev.ID}

	var files []git.FileDiff
	var oldRev, newRev string
	switch nt.Scope {
	case store.ScopeChange:
		if nt.Commit != "" || nt.Path != "" {
			return store.Location{}, errors.New("a change thread takes no commit or path")
		}
		return loc, nil
	case store.ScopeCommit:
		c, err := s.commitInRevision(ctx, rev, nt.Commit)
		if err != nil {
			return store.Location{}, err
		}
		loc.Commit = c.SHA
		if nt.Path == "" {
			return loc, nil
		}
		if nt.Path == CommitMsgPath {
			if side != anchor.SideNew {
				return store.Location{}, errors.New("commit messages only have a new side")
			}
			loc.Anchor = anchor.Anchor{Path: CommitMsgPath, Side: anchor.SideNew}
			if nt.StartLine == 0 {
				return loc, nil
			}
			a, err := anchor.Capture(CommitMsgPath, anchor.SideNew, anchor.SplitLines([]byte(c.Message)), nt.StartLine, nt.EndLine)
			if err != nil {
				return store.Location{}, err
			}
			loc.Anchor = a
			return loc, nil
		}
		if files, err = s.git.CommitDiff(ctx, c); err != nil {
			return store.Location{}, err
		}
		if len(c.Parents) > 0 {
			oldRev = c.Parents[0]
		}
		newRev = c.SHA
	case store.ScopeDiff:
		if nt.Commit != "" {
			return store.Location{}, errors.New("a diff thread takes no commit")
		}
		if nt.Path == "" {
			return store.Location{}, errors.New("a diff thread needs a path; use a change thread for the whole change")
		}
		if nt.Path == CommitMsgPath {
			return store.Location{}, errors.New("commit messages are commented on per commit")
		}
		var err error
		if files, err = s.RevisionFiles(ctx, rev); err != nil {
			return store.Location{}, err
		}
		oldRev, newRev = rev.BaseSHA, rev.HeadSHA
	default:
		return store.Location{}, fmt.Errorf("unknown scope %q", nt.Scope)
	}

	f, ok := findFile(files, side, nt.Path)
	if !ok {
		return store.Location{}, fmt.Errorf("%s is not on the %s side of the diff", nt.Path, side)
	}
	blob, err := s.blobID(ctx, side, oldRev, newRev, nt.Path)
	if err != nil {
		return store.Location{}, err
	}
	loc.BlobSHA = blob
	loc.Anchor = anchor.Anchor{Path: nt.Path, Side: side}
	if nt.StartLine == 0 {
		return loc, nil
	}
	if !anchor.InDiff(f, side, nt.StartLine, nt.EndLine) {
		return store.Location{}, fmt.Errorf("lines %d-%d of %s are not in the diff", nt.StartLine, nt.EndLine, nt.Path)
	}
	rev2 := newRev
	if side == anchor.SideOld {
		rev2 = oldRev
	}
	content, err := s.FileLines(ctx, rev2, nt.Path)
	if err != nil {
		return store.Location{}, err
	}
	a, err := anchor.Capture(nt.Path, side, content, nt.StartLine, nt.EndLine)
	if err != nil {
		return store.Location{}, err
	}
	loc.Anchor = a
	return loc, nil
}

// commitInRevision resolves a commit-ish and checks that it is one of the revision's commits.
func (s *Service) commitInRevision(ctx context.Context, rev store.Revision, commitish string) (git.Commit, error) {
	if commitish == "" {
		return git.Commit{}, errors.New("a commit thread needs a commit")
	}
	sha, err := s.git.ResolveCommit(ctx, commitish)
	if err != nil {
		return git.Commit{}, err
	}
	commits, err := s.Commits(ctx, rev)
	if err != nil {
		return git.Commit{}, err
	}
	for _, c := range commits {
		if c.SHA == sha {
			return c, nil
		}
	}
	return git.Commit{}, fmt.Errorf("commit %s is not in revision %d", shortSHA(sha), rev.Seq)
}

// Reply adds a comment to a thread. A draft reply joins the author's draft review. Otherwise it is
// published immediately.
func (s *Service) Reply(ctx context.Context, change store.Change, threadID, body string, draft bool) (store.Comment, error) {
	if strings.TrimSpace(body) == "" {
		return store.Comment{}, errors.New("comment body is empty")
	}
	var comment store.Comment
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		c := store.Comment{ThreadID: threadID, Author: s.author, Body: body}
		if draft {
			rev, err := tx.LatestRevision(ctx, change.ID)
			if err != nil {
				return err
			}
			review, err := s.draft(ctx, tx, change, rev)
			if err != nil {
				return err
			}
			c.ReviewID = review.ID
		}
		var err error
		comment, err = tx.AddComment(ctx, c)
		return err
	})
	return comment, err
}

// SetThreadState marks a thread open, addressed, or resolved.
func (s *Service) SetThreadState(ctx context.Context, threadID string, state store.ThreadState) error {
	return s.store.SetThreadState(ctx, threadID, state, s.author)
}

// EditComment replaces the body of one of the author's comments.
func (s *Service) EditComment(ctx context.Context, commentID, body string) error {
	c, err := s.store.GetComment(ctx, commentID)
	if err != nil {
		return err
	}
	if c.Author != s.author {
		return fmt.Errorf("comment %s is by %s, not %s", commentID, c.Author, s.author)
	}
	return s.store.EditComment(ctx, commentID, body)
}

// DeleteComment deletes one of the author's comments. Deleting the first comment of a draft thread
// deletes the thread.
func (s *Service) DeleteComment(ctx context.Context, commentID string) error {
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		c, err := tx.GetComment(ctx, commentID)
		if err != nil {
			return err
		}
		if c.Author != s.author {
			return fmt.Errorf("comment %s is by %s, not %s", commentID, c.Author, s.author)
		}
		t, err := tx.GetThread(ctx, c.ThreadID)
		if err != nil {
			return err
		}
		comments, err := tx.ListComments(ctx, t.ID, s.author)
		if err != nil {
			return err
		}
		if len(comments) > 0 && comments[0].ID == c.ID {
			if c.ReviewID != t.ReviewID {
				return errors.New("cannot delete the first comment of a thread")
			}
			review, err := tx.GetReview(ctx, t.ReviewID)
			if err != nil {
				return err
			}
			if review.State != store.ReviewDraft {
				return errors.New("cannot delete the first comment of a submitted thread")
			}
			return tx.DeleteThread(ctx, t.ID)
		}
		return tx.DeleteComment(ctx, c.ID)
	})
}

// Submit publishes the author's draft review with a verdict and summary. With no draft, a review
// with only the summary is created and submitted.
func (s *Service) Submit(ctx context.Context, change store.Change, verdict store.Verdict, body string) (store.Review, error) {
	switch verdict {
	case store.VerdictComment, store.VerdictApprove, store.VerdictRequestChanges:
	default:
		return store.Review{}, fmt.Errorf("verdict must be comment, approve, or request_changes, not %q", verdict)
	}
	var review store.Review
	err := s.store.Tx(ctx, func(tx *store.Tx) error {
		rev, err := tx.LatestRevision(ctx, change.ID)
		if err != nil {
			return err
		}
		draft, err := s.draft(ctx, tx, change, rev)
		if err != nil {
			return err
		}
		if body == "" {
			body = draft.Body
		}
		if err := tx.SubmitReview(ctx, draft.ID, rev.ID, verdict, body); err != nil {
			return err
		}
		if review, err = tx.GetReview(ctx, draft.ID); err != nil {
			return err
		}
		if strings.TrimSpace(body) != "" {
			_, err = tx.CreateSummaryThread(ctx, review)
		}
		return err
	})
	return review, err
}

// Draft returns the author's draft review on a change, if any.
func (s *Service) Draft(ctx context.Context, change store.Change) (store.Review, bool, error) {
	r, err := s.store.DraftReview(ctx, change.ID, s.author)
	if errors.Is(err, store.ErrNotFound) {
		return store.Review{}, false, nil
	}
	return r, err == nil, err
}

// SetDraftBody sets the summary of the author's draft review, creating the draft if needed.
func (s *Service) SetDraftBody(ctx context.Context, change store.Change, body string) error {
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		rev, err := tx.LatestRevision(ctx, change.ID)
		if err != nil {
			return err
		}
		draft, err := s.draft(ctx, tx, change, rev)
		if err != nil {
			return err
		}
		return tx.SetReviewBody(ctx, draft.ID, body)
	})
}

// Discard deletes the author's draft review and everything in it.
func (s *Service) Discard(ctx context.Context, change store.Change) error {
	r, ok, err := s.Draft(ctx, change)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("no draft review to discard")
	}
	return s.store.DeleteReview(ctx, r.ID)
}

// Reviews lists a change's submitted reviews and the author's draft.
func (s *Service) Reviews(ctx context.Context, change store.Change) ([]store.Review, error) {
	return s.store.ListReviews(ctx, change.ID, s.author)
}

// ThreadByRef finds a thread in a change by number or id prefix.
func (s *Service) ThreadByRef(ctx context.Context, change store.Change, ref string) (ThreadView, error) {
	ref = strings.TrimPrefix(ref, "#")
	var t store.Thread
	var err error
	if seq, convErr := strconv.Atoi(ref); convErr == nil {
		t, err = s.store.ThreadBySeq(ctx, change.ID, seq)
	} else {
		t, err = s.store.ResolveThreadPrefix(ctx, change.ID, ref)
	}
	if err != nil {
		return ThreadView{}, err
	}
	views, err := s.withComments(ctx, []store.Thread{t})
	if err != nil {
		return ThreadView{}, err
	}
	if len(views[0].Comments) == 0 {
		// Another author's draft.
		return ThreadView{}, fmt.Errorf("thread %s: %w", ref, store.ErrNotFound)
	}
	return views[0], nil
}

// ThreadFilter selects threads to list.
type ThreadFilter struct {
	States   []store.ThreadState
	Outdated *bool
}

// Threads lists a change's threads with their comments.
func (s *Service) Threads(ctx context.Context, change store.Change, f ThreadFilter) ([]ThreadView, error) {
	threads, err := s.store.ListThreads(ctx, store.ThreadFilter{
		ChangeID: change.ID, States: f.States, Outdated: f.Outdated, Viewer: s.author,
	})
	if err != nil {
		return nil, err
	}
	return s.withComments(ctx, threads)
}

// CommitThreads lists threads anchored to a commit, across all changes in the repository.
func (s *Service) CommitThreads(ctx context.Context, commitish string, f ThreadFilter) ([]ThreadView, error) {
	sha, err := s.git.ResolveCommit(ctx, commitish)
	if err != nil {
		return nil, err
	}
	threads, err := s.store.ListThreads(ctx, store.ThreadFilter{
		Commit: sha, States: f.States, Outdated: f.Outdated, Viewer: s.author,
	})
	if err != nil {
		return nil, err
	}
	// The commit index spans repositories that share history, so keep this repository's changes.
	var mine []store.Thread
	for _, t := range threads {
		c, err := s.store.GetChange(ctx, t.ChangeID)
		if err != nil {
			return nil, err
		}
		if c.RepoID == s.repo.ID {
			mine = append(mine, t)
		}
	}
	return s.withComments(ctx, mine)
}

func (s *Service) withComments(ctx context.Context, threads []store.Thread) ([]ThreadView, error) {
	views := make([]ThreadView, 0, len(threads))
	for _, t := range threads {
		comments, err := s.store.ListComments(ctx, t.ID, s.author)
		if err != nil {
			return nil, err
		}
		views = append(views, ThreadView{Thread: t, Comments: comments})
	}
	return views, nil
}
