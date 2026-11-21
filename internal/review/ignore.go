package review

import (
	"context"
	"path/filepath"

	"github.com/chancez/gr/internal/ignore"
)

// IgnoreFile is the repository's ignore file, at the top of the worktree.
const IgnoreFile = ".grignore"

// Ignored reads the patterns of the files a review leaves collapsed, as gitignore's syntax: the
// global file, then the worktree's IgnoreFile, then info/grignore in the git directory, for
// patterns of one's own. A later file's patterns override an earlier's. The files are read as they
// are on disk, so an edit applies on the next read.
func (s *Service) Ignored(ctx context.Context) (*ignore.Matcher, error) {
	common, err := s.git.CommonDir(ctx)
	if err != nil {
		return nil, err
	}
	paths := []string{filepath.Join(s.git.Dir, IgnoreFile), filepath.Join(common, "info", "grignore")}
	if s.globalIgnore != "" {
		paths = append([]string{s.globalIgnore}, paths...)
	}
	return ignore.Load(paths...)
}
