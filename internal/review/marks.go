package review

import (
	"cmp"
	"context"
	"slices"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
)

// CommitMark marks a whole commit reviewed.
func CommitMark(commit string) store.Mark { return store.Mark{Commit: commit} }

// CommitFileMark marks one file of a commit reviewed.
func CommitFileMark(commit string, f git.FileDiff) store.Mark {
	return store.Mark{Commit: commit, Path: f.Path()}
}

// DiffFileMark marks a file of the change's whole diff reviewed, at its current content.
func DiffFileMark(f git.FileDiff) store.Mark {
	return store.Mark{Path: f.Path(), Blob: cmp.Or(f.NewBlob, f.OldBlob)}
}

// Marks is the set of things the author marked reviewed on a change.
type Marks map[store.Mark]bool

// CommitReviewed reports whether a commit is marked reviewed, or all of its files are.
func (m Marks) CommitReviewed(commit string, files []git.FileDiff) bool {
	if m[CommitMark(commit)] {
		return true
	}
	for _, f := range files {
		if !m[CommitFileMark(commit, f)] {
			return false
		}
	}
	return len(files) > 0
}

// FileReviewed reports whether a file of a commit is reviewed, directly or through its commit.
func (m Marks) FileReviewed(commit string, f git.FileDiff) bool {
	return m[CommitMark(commit)] || m[CommitFileMark(commit, f)]
}

// Marks returns what the author has marked reviewed on a change.
func (s *Service) Marks(ctx context.Context, change store.Change) (Marks, error) {
	list, err := s.store.ListMarks(ctx, change.ID, s.author)
	if err != nil {
		return nil, err
	}
	marks := Marks{}
	for _, m := range list {
		marks[m] = true
	}
	return marks, nil
}

// SetReviewed marks or unmarks something reviewed.
func (s *Service) SetReviewed(ctx context.Context, change store.Change, m store.Mark, reviewed bool) error {
	if reviewed {
		return s.store.AddMark(ctx, change.ID, s.author, m)
	}
	return s.store.DeleteMark(ctx, change.ID, s.author, m)
}

// SetCommitReviewed marks or unmarks a whole commit. Unmarking also clears its file marks, so the
// commit reads as unreviewed afterwards.
func (s *Service) SetCommitReviewed(ctx context.Context, change store.Change, commit string, files []git.FileDiff, reviewed bool) error {
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		if reviewed {
			return tx.AddMark(ctx, change.ID, s.author, CommitMark(commit))
		}
		if err := tx.DeleteMark(ctx, change.ID, s.author, CommitMark(commit)); err != nil {
			return err
		}
		for _, f := range files {
			if err := tx.DeleteMark(ctx, change.ID, s.author, CommitFileMark(commit, f)); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetFileReviewed marks or unmarks file f reviewed: f of commit, with files the commit's files, or
// of the whole diff when commit is empty. Unmarking one file of a reviewed commit marks its other
// files, so they stay reviewed.
func (s *Service) SetFileReviewed(ctx context.Context, change store.Change, commit string, files []git.FileDiff, f git.FileDiff, reviewed bool) error {
	if commit == "" {
		return s.SetReviewed(ctx, change, DiffFileMark(f), reviewed)
	}
	return s.store.Tx(ctx, func(tx *store.Tx) error {
		if reviewed {
			return tx.AddMark(ctx, change.ID, s.author, CommitFileMark(commit, f))
		}
		list, err := tx.ListMarks(ctx, change.ID, s.author)
		if err != nil {
			return err
		}
		if slices.Contains(list, CommitMark(commit)) {
			if err := tx.DeleteMark(ctx, change.ID, s.author, CommitMark(commit)); err != nil {
				return err
			}
			for _, other := range files {
				if other.Path() == f.Path() {
					continue
				}
				if err := tx.AddMark(ctx, change.ID, s.author, CommitFileMark(commit, other)); err != nil {
					return err
				}
			}
		}
		return tx.DeleteMark(ctx, change.ID, s.author, CommitFileMark(commit, f))
	})
}
