package review

import (
	"context"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
)

// Commits lists a revision's commits, oldest first.
func (s *Service) Commits(ctx context.Context, rev store.Revision) ([]git.Commit, error) {
	return s.git.Commits(ctx, rev.BaseSHA, rev.HeadSHA)
}

// CommitFiles returns a commit's diff, with its message first as CommitMsgPath.
func (s *Service) CommitFiles(ctx context.Context, c git.Commit) ([]git.FileDiff, error) {
	files, err := s.commitDiff(ctx, c)
	if err != nil {
		return nil, err
	}
	return append([]git.FileDiff{CommitMessageFile(c)}, files...), nil
}

func parent(c git.Commit) string {
	if len(c.Parents) > 0 {
		return c.Parents[0]
	}
	return ""
}

// commitDiff is a commit's diff against its first parent, through the cache.
func (s *Service) commitDiff(ctx context.Context, c git.Commit, paths ...string) ([]git.FileDiff, error) {
	return s.diff(ctx, parent(c), c.SHA, paths...)
}

// CommitFile is one file's diff in a commit, for when only that file is needed, as for a thread's
// excerpt.
func (s *Service) CommitFile(ctx context.Context, c git.Commit, path string) ([]git.FileDiff, error) {
	return s.commitDiff(ctx, c, path)
}

// CommitFileLists lists the files each commit of a revision changes, without hunks, with each
// commit's message first as CommitMsgPath, from one git process.
func (s *Service) CommitFileLists(ctx context.Context, rev store.Revision, commits []git.Commit) (map[string][]git.FileDiff, error) {
	key := rev.BaseSHA + ".." + rev.HeadSHA
	lists, ok := s.lists.get(key)
	if !ok {
		var err error
		if lists, err = s.git.CommitFiles(ctx, rev.BaseSHA, rev.HeadSHA); err != nil {
			return nil, err
		}
		s.lists.put(key, lists)
	}
	out := make(map[string][]git.FileDiff, len(commits))
	for _, c := range commits {
		msg := CommitMessageFile(c)
		msg.Hunks = nil
		out[c.SHA] = append([]git.FileDiff{msg}, lists[c.SHA]...)
	}
	return out, nil
}

// RevisionFileList lists the files in a revision's whole diff, without hunks.
func (s *Service) RevisionFileList(ctx context.Context, rev store.Revision) ([]git.FileDiff, error) {
	key := "diff:" + rev.BaseSHA + ".." + rev.HeadSHA
	if lists, ok := s.lists.get(key); ok {
		return lists[""], nil
	}
	files, err := s.git.DiffFiles(ctx, rev.BaseSHA, rev.HeadSHA)
	if err != nil {
		return nil, err
	}
	s.lists.put(key, map[string][]git.FileDiff{"": files})
	return files, nil
}

// RevisionFiles returns a revision's whole diff, through the cache.
func (s *Service) RevisionFiles(ctx context.Context, rev store.Revision) ([]git.FileDiff, error) {
	return s.diff(ctx, rev.BaseSHA, rev.HeadSHA)
}

// Interdiff returns what changed between two revisions of a change, excluding changes that came only
// from a moved base.
//
// When the base moved, the older revision's changes are replayed onto the newer base with a
// three-way merge, and the result is compared to the newer head. If the replay conflicts, the heads
// are compared directly, which includes the base's changes.
func (s *Service) Interdiff(ctx context.Context, from, to store.Revision) (files []git.FileDiff, exact bool, err error) {
	if from.BaseSHA == to.BaseSHA || from.BaseSHA == "" || to.BaseSHA == "" {
		files, err = s.diff(ctx, from.HeadSHA, to.HeadSHA)
		return files, true, err
	}
	tree, clean, err := s.git.MergeTree(ctx, from.BaseSHA, to.BaseSHA, from.HeadSHA)
	if err != nil {
		return nil, false, err
	}
	if !clean {
		files, err = s.diff(ctx, from.HeadSHA, to.HeadSHA)
		return files, false, err
	}
	files, err = s.diff(ctx, tree, to.HeadSHA)
	return files, true, err
}

// CommitMessageFile presents a commit message as an added file.
func CommitMessageFile(c git.Commit) git.FileDiff {
	lines := anchor.SplitLines([]byte(c.Message))
	h := git.Hunk{NewStart: 1, NewLines: len(lines)}
	for i, text := range lines {
		h.Lines = append(h.Lines, git.Line{Kind: git.LineAdded, Text: text, NewLine: i + 1})
	}
	f := git.FileDiff{NewPath: CommitMsgPath, Status: git.StatusAdded}
	if len(lines) > 0 {
		f.Hunks = []git.Hunk{h}
	}
	return f
}

// FileLines reads a file at rev as lines. A missing file or empty rev has no lines.
func (s *Service) FileLines(ctx context.Context, rev, path string) ([]string, error) {
	if rev == "" {
		return nil, nil
	}
	content, _, err := s.git.ReadFile(ctx, rev, path)
	if err != nil {
		return nil, err
	}
	return anchor.SplitLines(content), nil
}

// findFile returns the diff for path on side, matching the old path on the old side so renames are
// followed.
func findFile(files []git.FileDiff, side anchor.Side, path string) (git.FileDiff, bool) {
	for _, f := range files {
		if (side == anchor.SideOld && f.OldPath == path) || (side != anchor.SideOld && f.NewPath == path) {
			return f, true
		}
	}
	return git.FileDiff{}, false
}

// touches reports whether any file in files has path on either side.
func touches(files []git.FileDiff, path string) bool {
	for _, f := range files {
		if f.OldPath == path || f.NewPath == path {
			return true
		}
	}
	return false
}
