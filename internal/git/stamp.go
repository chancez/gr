package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A stamp tells, by stat alone, that what git reads from some files may have changed, so that
// git need not run to find nothing did. git replaces a file to change it, a loose ref or HEAD,
// which changes the file and the directory it is in.

// settle is how long since a file changed that a stamp of it can be trusted: a file changed again
// within the same tick of its modification time would look unchanged.
const settle = 2 * time.Second

// Stamp is the size and modification time of each of paths, or "" when one changed too recently
// to tell a later change by.
func Stamp(paths []string) string {
	var b strings.Builder
	now := time.Now()
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			b.WriteString("-\n")
			continue
		}
		if now.Sub(info.ModTime()) < settle {
			return ""
		}
		fmt.Fprintf(&b, "%d %d\n", info.ModTime().UnixNano(), info.Size())
	}
	return b.String()
}

// RefFiles are the files refs are read from in resolving refs, or finding the default branch:
// the config, packed refs, the reftable, the directories of local branches and of each remote's,
// and the loose ref each of refs, by full or short name, may be.
func (r *Repo) RefFiles(ctx context.Context, refs []string) ([]string, error) {
	common, err := commonDir(ctx, r.Dir)
	if err != nil {
		return nil, err
	}
	files := []string{
		filepath.Join(common, "config"),
		filepath.Join(common, "packed-refs"),
		filepath.Join(common, "reftable", "tables.list"),
		filepath.Join(common, "refs", "heads"),
		filepath.Join(common, "refs", "remotes"),
	}
	remotes, _ := os.ReadDir(filepath.Join(common, "refs", "remotes"))
	for _, e := range remotes {
		files = append(files, filepath.Join(common, "refs", "remotes", e.Name()))
	}
	for _, ref := range refs {
		if strings.HasPrefix(ref, "refs/") {
			files = append(files, filepath.Join(common, filepath.FromSlash(ref)))
			continue
		}
		files = append(files,
			filepath.Join(common, "refs", "heads", filepath.FromSlash(ref)),
			filepath.Join(common, "refs", "remotes", filepath.FromSlash(ref)))
	}
	return files, nil
}

// WorktreeFiles are the files Worktrees reads: the main worktree's HEAD, and the directory of the
// others and each one's HEAD.
func (r *Repo) WorktreeFiles(ctx context.Context) ([]string, error) {
	common, err := commonDir(ctx, r.Dir)
	if err != nil {
		return nil, err
	}
	files := []string{
		filepath.Join(common, "HEAD"),
		filepath.Join(common, "worktrees"),
	}
	trees, _ := os.ReadDir(filepath.Join(common, "worktrees"))
	for _, e := range trees {
		files = append(files, filepath.Join(common, "worktrees", e.Name(), "HEAD"))
	}
	return files, nil
}
