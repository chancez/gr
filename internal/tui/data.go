package tui

import (
	"context"
	"errors"
	"maps"
	"strings"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// changeData is what the TUI shows for a change. Per ADR 9, file lists load eagerly and diff
// contents lazily: commitFiles, diffFiles, and interdiff hold file lists without hunks until their
// view is loaded, which loaded records by view key.
type changeData struct {
	change    store.Change
	revision  store.Revision
	revisions []store.Revision
	// baseTip is the base branch's commit when the change was loaded, to see it move, as when the
	// branch is merged into it.
	baseTip     string
	commits     []git.Commit
	commitFiles map[string][]git.FileDiff
	diffFiles   []git.FileDiff
	loaded      map[string]bool
	// threadFiles are the diffs, of just the thread's file, that excerpts are cut from.
	threadFiles map[string][]git.FileDiff
	threads     []review.ThreadView
	reviews     []store.Review
	// events are what happened to the change, oldest first.
	events []store.Event
	// diffPlacements is where threads on commits show in the whole diff, by thread id.
	diffPlacements map[string]store.Location
	// marks is what the viewer marked reviewed.
	marks review.Marks
	// draftReviewIDs marks the reviews that are the viewer's draft, so their threads show as drafts.
	draftReviewIDs map[string]bool

	// hasInterdiff is set when there is an earlier revision to compare with; interdiff is loaded
	// with its view.
	hasInterdiff  bool
	interdiff     []git.FileDiff
	interdiffFrom store.Revision
	// interdiffTo is the revision the interdiff compares with: the latest, unless two others were
	// picked to compare.
	interdiffTo store.Revision
	// interdiffFromReview is set when interdiffFrom is the revision the viewer last reviewed.
	interdiffFromReview bool
	interdiffExact      bool
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

// load reads a change: git's file lists, which need one process each, then what the database holds.
// Diff contents are left to loadView.
func load(ctx context.Context, svc *review.Service, changeID string) (*changeData, error) {
	change, err := svc.Store().GetChange(ctx, changeID)
	if err != nil {
		return nil, err
	}
	// The base's tip, read before syncing: should it move meanwhile, the next look sees it did.
	var baseTip string
	if change.Kind == store.KindBranch {
		baseTip, _ = svc.Git().ResolveCommit(ctx, change.BaseBranch)
	}
	change, rev, err := svc.Sync(ctx, change)
	if err != nil {
		return nil, err
	}
	d := &changeData{change: change, revision: rev, baseTip: baseTip, loaded: map[string]bool{}, diffPlacements: map[string]store.Location{}}
	if d.revisions, err = svc.Revisions(ctx, change); err != nil {
		return nil, err
	}
	if d.commits, err = svc.Commits(ctx, rev); err != nil {
		return nil, err
	}
	if d.commitFiles, err = svc.CommitFileLists(ctx, rev, d.commits); err != nil {
		return nil, err
	}
	if d.diffFiles, err = svc.RevisionFileList(ctx, rev); err != nil {
		return nil, err
	}
	if err := loadDB(ctx, svc, d); err != nil {
		return nil, err
	}
	if from, fromReview, ok := interdiffBase(d, svc.Author()); ok {
		d.hasInterdiff, d.interdiffFrom, d.interdiffFromReview = true, from, fromReview
	}
	d.interdiffTo = rev
	return d, nil
}

// reload re-reads only what the database holds, keeping git's data, loaded diffs included, from
// old. It is for after an action, when the branch has not moved.
func reload(ctx context.Context, svc *review.Service, old *changeData) (*changeData, error) {
	change, err := svc.Store().GetChange(ctx, old.change.ID)
	if err != nil {
		return nil, err
	}
	d := *old
	d.change = change
	d.loaded = maps.Clone(old.loaded)
	if d.revisions, err = svc.Revisions(ctx, change); err != nil {
		return nil, err
	}
	if err := loadDB(ctx, svc, &d); err != nil {
		return nil, err
	}
	if d.loaded[view{kind: viewDiff}.key()] {
		if d.diffPlacements, err = placeInDiff(ctx, svc, &d, d.diffFiles); err != nil {
			return nil, err
		}
	}
	return &d, nil
}

// loadDB reads threads, reviews, and marks, and the per-file diffs their excerpts need.
func loadDB(ctx context.Context, svc *review.Service, d *changeData) error {
	var err error
	if d.threads, err = svc.Threads(ctx, d.change, review.ThreadFilter{}); err != nil {
		return err
	}
	if d.reviews, err = svc.Reviews(ctx, d.change); err != nil {
		return err
	}
	if d.events, err = svc.Events(ctx, d.change); err != nil {
		return err
	}
	if d.marks, err = svc.Marks(ctx, d.change); err != nil {
		return err
	}
	d.draftReviewIDs = map[string]bool{}
	for _, r := range d.reviews {
		if r.State == store.ReviewDraft {
			d.draftReviewIDs[r.ID] = true
		}
	}
	commits := map[string]git.Commit{}
	for _, c := range d.commits {
		commits[c.SHA] = c
	}
	d.threadFiles = map[string][]git.FileDiff{}
	for _, t := range d.threads {
		loc := t.Location
		if t.Outdated || loc.StartLine == 0 || loc.Path == "" || loc.Path == review.CommitMsgPath {
			continue
		}
		var files []git.FileDiff
		switch {
		case loc.Scope == store.ScopeCommit:
			c, ok := commits[loc.Commit]
			if !ok {
				continue
			}
			if files, err = svc.CommitFile(ctx, c, loc.Path); err != nil {
				return err
			}
		case loc.Scope == store.ScopeDiff && loc.RevisionID == d.revision.ID:
			if files, err = svc.RevisionFile(ctx, d.revision, loc.Path); err != nil {
				return err
			}
		}
		d.threadFiles[t.ID] = files
	}
	return nil
}

// viewLoadedMsg carries a view's diff contents, read by loadView.
type viewLoadedMsg struct {
	key        string
	head       string
	files      []git.FileDiff
	placements map[string]store.Location
	exact      bool
	err        error
}

// loadView reads a view's diff contents, through the service's cache.
func loadView(ctx context.Context, svc *review.Service, d *changeData, v view) viewLoadedMsg {
	msg := viewLoadedMsg{key: v.key(), head: d.revision.HeadSHA}
	switch v.kind {
	case viewCommit:
		msg.files, msg.err = svc.CommitFiles(ctx, v.commit)
	case viewDiff:
		if msg.files, msg.err = svc.RevisionFiles(ctx, d.revision); msg.err == nil {
			msg.placements, msg.err = placeInDiff(ctx, svc, d, msg.files)
		}
	case viewInterdiff:
		msg.files, msg.exact, msg.err = svc.Interdiff(ctx, d.interdiffFrom, d.interdiffTo)
	}
	return msg
}

// apply stores a loaded view's diff contents.
func (d *changeData) apply(msg viewLoadedMsg) {
	for _, c := range d.commits {
		if "commit:"+c.SHA == msg.key {
			d.commitFiles[c.SHA] = msg.files
		}
	}
	switch msg.key {
	case view{kind: viewDiff}.key():
		d.diffFiles, d.diffPlacements = msg.files, msg.placements
	case view{kind: viewInterdiff}.key():
		d.interdiff, d.interdiffExact = msg.files, msg.exact
	}
	d.loaded[msg.key] = true
}

// carryLoaded keeps the diffs old had loaded that are still valid in d, so a reload does not drop
// them and redraw the view without its hunks. A commit's diff never changes, so it carries over as
// long as the commit is still in the change. The whole diff and the interdiff depend on the
// revision, so they carry over only within one.
func (d *changeData) carryLoaded(old *changeData) {
	if old == nil {
		return
	}
	sameRevision := old.revision.ID == d.revision.ID
	for key := range old.loaded {
		switch {
		case key == view{kind: viewDiff}.key():
			if !sameRevision {
				continue
			}
			d.diffFiles, d.diffPlacements = old.diffFiles, old.diffPlacements
		case key == view{kind: viewInterdiff}.key():
			if !sameRevision || old.interdiffFrom.ID != d.interdiffFrom.ID || old.interdiffTo.ID != d.interdiffTo.ID {
				continue
			}
			d.interdiff, d.interdiffExact = old.interdiff, old.interdiffExact
		default:
			sha := strings.TrimPrefix(key, "commit:")
			files, ok := old.commitFiles[sha]
			if _, inChange := d.commitFiles[sha]; !ok || !inChange {
				continue
			}
			d.commitFiles[sha] = files
		}
		d.loaded[key] = true
	}
}

// preload loads the view keyed current, so a full reload shows it with its diff rather than
// redrawing it without hunks while the diff loads.
func (d *changeData) preload(ctx context.Context, svc *review.Service, current string) error {
	if current == "" || d.loaded[current] {
		return nil
	}
	for _, v := range buildViews(d) {
		if v.key() == current && v.kind != viewOverview {
			msg := loadView(ctx, svc, d, v)
			if msg.err != nil {
				return msg.err
			}
			d.apply(msg)
		}
	}
	return nil
}

// placeInDiff maps current line threads on commits to the whole diff, given as files: their
// new-side lines carried from the commit to the head, kept when they land in the diff.
func placeInDiff(ctx context.Context, svc *review.Service, d *changeData, diffFiles []git.FileDiff) (map[string]store.Location, error) {
	out := map[string]store.Location{}
	for _, t := range d.threads {
		loc := t.Location
		if t.Outdated || loc.Scope != store.ScopeCommit || loc.StartLine == 0 || loc.Side != anchor.SideNew ||
			loc.Path == review.CommitMsgPath {
			continue
		}
		start, end, err := svc.TranslateLines(ctx, loc.Commit, d.revision.HeadSHA, loc.Path, loc.StartLine, loc.EndLine)
		if err != nil {
			return nil, err
		}
		for _, f := range diffFiles {
			if f.NewPath == loc.Path && anchor.InDiff(f, anchor.SideNew, start, end) {
				placed := loc
				placed.Scope, placed.StartLine, placed.EndLine = store.ScopeDiff, start, end
				out[t.ID] = placed
			}
		}
	}
	return out, nil
}

// compare sets the interdiff to compare revisions from and to, numbered by seq, and reports whether
// both are revisions of the change. Its diff is loaded again.
func (d *changeData) compare(from, to int) bool {
	var f, t *store.Revision
	for i := range d.revisions {
		switch d.revisions[i].Seq {
		case from:
			f = &d.revisions[i]
		case to:
			t = &d.revisions[i]
		}
	}
	if f == nil || t == nil {
		return false
	}
	d.hasInterdiff, d.interdiffFrom, d.interdiffTo, d.interdiffFromReview = true, *f, *t, false
	d.interdiff, d.interdiffExact = nil, false
	delete(d.loaded, view{kind: viewInterdiff}.key())
	return true
}

// interdiffBase picks the revision to compare the latest with: the one the viewer last reviewed,
// else the previous revision.
func interdiffBase(d *changeData, viewer store.Author) (rev store.Revision, fromReview, ok bool) {
	if len(d.revisions) < 2 {
		return store.Revision{}, false, false
	}
	byID := map[string]store.Revision{}
	for _, r := range d.revisions {
		byID[r.ID] = r
	}
	for i := len(d.reviews) - 1; i >= 0; i-- {
		r := d.reviews[i]
		if r.Author == viewer && r.State == store.ReviewSubmitted && r.RevisionID != d.revision.ID {
			if rev, ok := byID[r.RevisionID]; ok {
				return rev, true, true
			}
		}
	}
	return d.revisions[len(d.revisions)-2], false, true
}

var errNoDraft = errors.New("no draft review")
