package tui

import (
	"context"
	"errors"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// changeData is everything the TUI shows for a change, loaded in one pass.
type changeData struct {
	change      store.Change
	revision    store.Revision
	revisions   []store.Revision
	commits     []git.Commit
	commitFiles map[string][]git.FileDiff
	diffFiles   []git.FileDiff
	threads     []review.ThreadView
	reviews     []store.Review
	// marks is what the viewer marked reviewed.
	marks review.Marks
	// draftReviewIDs marks the reviews that are the viewer's draft, so their threads show as drafts.
	draftReviewIDs map[string]bool

	// interdiff is set when there is an earlier revision to compare with.
	interdiff      []git.FileDiff
	interdiffFrom  store.Revision
	interdiffExact bool
}

// draft returns the viewer's draft review, if any.
func (d *changeData) draft() (store.Review, bool) {
	for _, r := range d.reviews {
		if r.State == store.ReviewDraft {
			return r, true
		}
	}
	return store.Review{}, false
}

func (d *changeData) isDraft(reviewID string) bool { return d.draftReviewIDs[reviewID] }

func load(ctx context.Context, svc *review.Service, changeID string) (*changeData, error) {
	change, err := svc.Store().GetChange(ctx, changeID)
	if err != nil {
		return nil, err
	}
	change, rev, err := svc.Sync(ctx, change)
	if err != nil {
		return nil, err
	}
	d := &changeData{change: change, revision: rev, commitFiles: map[string][]git.FileDiff{}, draftReviewIDs: map[string]bool{}}
	if d.revisions, err = svc.Revisions(ctx, change); err != nil {
		return nil, err
	}
	if d.commits, err = svc.Commits(ctx, rev); err != nil {
		return nil, err
	}
	for _, c := range d.commits {
		files, err := svc.CommitFiles(ctx, c)
		if err != nil {
			return nil, err
		}
		d.commitFiles[c.SHA] = files
	}
	if d.diffFiles, err = svc.RevisionFiles(ctx, rev); err != nil {
		return nil, err
	}
	if d.threads, err = svc.Threads(ctx, change, review.ThreadFilter{}); err != nil {
		return nil, err
	}
	if d.reviews, err = svc.Reviews(ctx, change); err != nil {
		return nil, err
	}
	if d.marks, err = svc.Marks(ctx, change); err != nil {
		return nil, err
	}
	for _, r := range d.reviews {
		if r.State == store.ReviewDraft {
			d.draftReviewIDs[r.ID] = true
		}
	}
	if from, ok := interdiffBase(d, svc.Author()); ok {
		files, exact, err := svc.Interdiff(ctx, from, rev)
		if err != nil {
			return nil, err
		}
		d.interdiff, d.interdiffFrom, d.interdiffExact = files, from, exact
	}
	return d, nil
}

// interdiffBase picks the revision to compare the latest with: the one the viewer last reviewed,
// else the previous revision.
func interdiffBase(d *changeData, viewer store.Author) (store.Revision, bool) {
	if len(d.revisions) < 2 {
		return store.Revision{}, false
	}
	byID := map[string]store.Revision{}
	for _, r := range d.revisions {
		byID[r.ID] = r
	}
	for i := len(d.reviews) - 1; i >= 0; i-- {
		r := d.reviews[i]
		if r.Author == viewer && r.State == store.ReviewSubmitted && r.RevisionID != d.revision.ID {
			if rev, ok := byID[r.RevisionID]; ok {
				return rev, true
			}
		}
	}
	return d.revisions[len(d.revisions)-2], true
}

var errNoDraft = errors.New("no draft review")
