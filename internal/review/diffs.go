package review

import (
	"context"
	"slices"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
)

// Commits lists a revision's commits, oldest first.
func (s *Service) Commits(ctx context.Context, rev store.Revision) ([]git.Commit, error) {
	return s.git.Commits(ctx, rev.BaseSHA, rev.HeadSHA)
}

// CommitFiles returns a commit's diff with opts, with its message first as CommitMsgPath.
func (s *Service) CommitFiles(ctx context.Context, c git.Commit, opts git.DiffOptions) ([]git.FileDiff, error) {
	files, err := s.commitDiff(ctx, c, opts)
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

// commitDiff is a commit's diff against its first parent.
func (s *Service) commitDiff(ctx context.Context, c git.Commit, opts git.DiffOptions, paths ...string) ([]git.FileDiff, error) {
	return s.diff(ctx, parent(c), c.SHA, opts, paths...)
}

// CommitFile is one file's diff in a commit with opts, for when only that file is needed, as for a
// thread's excerpt or a file shown with options of its own.
func (s *Service) CommitFile(ctx context.Context, c git.Commit, path string, opts git.DiffOptions) ([]git.FileDiff, error) {
	return s.commitDiff(ctx, c, opts, path)
}

// diff is the diff from one revision to another with opts, limited to paths if any are given.
// Ignoring whitespace, git leaves out a file whose changes are all whitespace; it is kept, without
// hunks and with OnlyWhitespace set, so a diff names the same files whatever its options.
func (s *Service) diff(ctx context.Context, from, to string, opts git.DiffOptions, paths ...string) ([]git.FileDiff, error) {
	files, err := s.diffs.Diff(ctx, from, to, opts, paths...)
	if err != nil || !opts.IgnoreAllSpace {
		return files, err
	}
	list, err := s.diffs.DiffFiles(ctx, from, to)
	if err != nil {
		return nil, err
	}
	type key struct {
		old string
		new string
	}
	byKey := map[key]git.FileDiff{}
	for _, f := range files {
		byKey[key{f.OldPath, f.NewPath}] = f
	}
	out := make([]git.FileDiff, 0, len(list))
	for _, f := range list {
		if len(paths) > 0 && !slices.Contains(paths, f.OldPath) && !slices.Contains(paths, f.NewPath) {
			continue
		}
		k := key{f.OldPath, f.NewPath}
		if d, ok := byKey[k]; ok {
			out = append(out, d)
			delete(byKey, k)
			continue
		}
		f.Added, f.Deleted, f.OnlyWhitespace = 0, 0, true
		out = append(out, f)
	}
	// Whatever the list did not name, in the order git gave it.
	for _, f := range files {
		if _, ok := byKey[key{f.OldPath, f.NewPath}]; ok {
			out = append(out, f)
		}
	}
	return out, nil
}

// CommitFileLists lists the files each commit of a revision changes, without hunks, with each
// commit's message first as CommitMsgPath, from one git process.
func (s *Service) CommitFileLists(ctx context.Context, rev store.Revision, commits []git.Commit) (map[string][]git.FileDiff, error) {
	lists, err := s.diffs.CommitFiles(ctx, rev.BaseSHA, rev.HeadSHA)
	if err != nil {
		return nil, err
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
	return s.diffs.DiffFiles(ctx, rev.BaseSHA, rev.HeadSHA)
}

// RevisionFile is one file's diff in a revision's whole diff, with opts.
func (s *Service) RevisionFile(ctx context.Context, rev store.Revision, path string, opts git.DiffOptions) ([]git.FileDiff, error) {
	return s.diff(ctx, rev.BaseSHA, rev.HeadSHA, opts, path)
}

// RevisionFiles returns a revision's whole diff, with opts.
func (s *Service) RevisionFiles(ctx context.Context, rev store.Revision, opts git.DiffOptions) ([]git.FileDiff, error) {
	return s.diff(ctx, rev.BaseSHA, rev.HeadSHA, opts)
}

// Interdiff returns what changed between two revisions of a change, with opts, excluding changes that
// came only from a moved base.
//
// When the base moved, the older revision's changes are replayed onto the newer base with a
// three-way merge, and the result is compared to the newer head. If the replay conflicts, the heads
// are compared directly, which includes the base's changes.
func (s *Service) Interdiff(ctx context.Context, from, to store.Revision, opts git.DiffOptions) (files []git.FileDiff, exact bool, err error) {
	if from.BaseSHA == to.BaseSHA || from.BaseSHA == "" || to.BaseSHA == "" {
		files, err = s.diff(ctx, from.HeadSHA, to.HeadSHA, opts)
		return files, true, err
	}
	tree, clean, err := s.git.MergeTree(ctx, from.BaseSHA, to.BaseSHA, from.HeadSHA)
	if err != nil {
		return nil, false, err
	}
	if !clean {
		files, err = s.diff(ctx, from.HeadSHA, to.HeadSHA, opts)
		return files, false, err
	}
	files, err = s.diff(ctx, tree, to.HeadSHA, opts)
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
