package review

import (
	"bytes"
	"context"
	"path"

	"github.com/chancez/gr/internal/git"
)

// firstLineDiffs reads diffs from a repository, setting FirstLine on each file with no extension
// whose hunks do not reach its first line, so its type can be told from a #! line.
type firstLineDiffs struct {
	*git.Repo
}

func (d firstLineDiffs) Diff(ctx context.Context, from, to string, opts git.DiffOptions, paths ...string) ([]git.FileDiff, error) {
	files, err := d.Repo.Diff(ctx, from, to, opts, paths...)
	if err != nil {
		return nil, err
	}
	for i, f := range files {
		rev, start := to, func(h git.Hunk) int { return h.NewStart }
		if f.NewPath == "" {
			rev, start = from, func(h git.Hunk) int { return h.OldStart }
		}
		if f.Binary || path.Ext(f.Path()) != "" || (len(f.Hunks) > 0 && start(f.Hunks[0]) <= 1) {
			continue
		}
		content, _, err := d.ReadFile(ctx, rev, f.Path())
		if err != nil {
			return nil, err
		}
		line, _, _ := bytes.Cut(content, []byte("\n"))
		files[i].FirstLine = string(bytes.TrimSuffix(line, []byte("\r")))
	}
	return files, nil
}
