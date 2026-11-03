package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

func (a *app) markCommand(reviewed bool) *cobra.Command {
	var cf changeFlags
	var commit string
	var paths []string
	use, verb := "mark", "Mark"
	if !reviewed {
		use, verb = "unmark", "Unmark"
	}
	cmd := &cobra.Command{
		Use:   use + " [target]",
		Short: verb + " a commit or a file reviewed",
		Long: verb + ` a commit or a file reviewed, as m and M do in the TUI.

With --commit alone the whole commit is ` + use + `ed. -p narrows it to files, and the commit
message is the path ` + review.CommitMsgPath + `. Without --commit, -p ` + use + `s files of the change's whole
diff, where a mark lapses when the file's content changes. gr change show lists what is marked.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if commit == "" && len(paths) == 0 {
				return errors.New("pass --commit, -p, or both")
			}
			svc, change, rev, err := a.resolveChange(ctx, cf, firstArg(args))
			if err != nil {
				return err
			}
			var what []string
			if commit == "" {
				files, err := svc.RevisionFileList(ctx, rev)
				if err != nil {
					return err
				}
				for _, p := range paths {
					f, ok := fileByPath(files, p)
					if !ok {
						return fmt.Errorf("%s is not in the change's diff", p)
					}
					if err := svc.SetFileReviewed(ctx, change, "", files, f, reviewed); err != nil {
						return err
					}
					what = append(what, "diff "+f.Path())
				}
			} else {
				c, err := svc.CommitInRevision(ctx, rev, commit)
				if err != nil {
					return err
				}
				lists, err := svc.CommitFileLists(ctx, rev, []git.Commit{c})
				if err != nil {
					return err
				}
				files := lists[c.SHA]
				if len(paths) == 0 {
					if err := svc.SetCommitReviewed(ctx, change, c.SHA, files, reviewed); err != nil {
						return err
					}
					what = append(what, "commit "+short(c.SHA))
				}
				for _, p := range paths {
					f, ok := fileByPath(files, p)
					if !ok {
						return fmt.Errorf("%s is not in commit %s", p, short(c.SHA))
					}
					if err := svc.SetFileReviewed(ctx, change, c.SHA, files, f, reviewed); err != nil {
						return err
					}
					what = append(what, "commit "+short(c.SHA)+" "+f.Path())
				}
			}
			for _, w := range what {
				if reviewed {
					fmt.Fprintf(a.stderr, "%s marked reviewed\n", w)
				} else {
					fmt.Fprintf(a.stderr, "%s marked unreviewed\n", w)
				}
			}
			if a.json {
				out, err := reviewedStatus(ctx, svc, change, rev)
				if err != nil {
					return err
				}
				return a.printJSON(out)
			}
			return nil
		},
	}
	cf.register(cmd)
	cmd.Flags().StringVar(&commit, "commit", "", "commit to "+use)
	cmd.Flags().StringArrayVarP(&paths, "path", "p", nil, "file to "+use+", or "+review.CommitMsgPath+"; repeatable")
	return cmd
}

// fileByPath finds a file by its new or old path.
func fileByPath(files []git.FileDiff, path string) (git.FileDiff, bool) {
	for _, f := range files {
		if f.NewPath == path || f.OldPath == path {
			return f, true
		}
	}
	return git.FileDiff{}, false
}

// reviewedJSON is what the author marked reviewed in a revision.
type reviewedJSON struct {
	Commits []commitReviewedJSON `json:"commits"`
	// Diff lists the files of the whole diff marked reviewed at their current content.
	Diff []string `json:"diff"`
	// DiffFiles counts the whole diff's files.
	DiffFiles int `json:"diff_files"`
}

type commitReviewedJSON struct {
	SHA      string `json:"sha"`
	Subject  string `json:"subject"`
	Reviewed bool   `json:"reviewed"`
	// Files lists the commit's files marked reviewed, when the commit as a whole is not.
	Files []string `json:"files,omitempty"`
}

// reviewedStatus works out what the author marked reviewed in a revision, commit by commit.
func reviewedStatus(ctx context.Context, svc *review.Service, change store.Change, rev store.Revision) (reviewedJSON, error) {
	marks, err := svc.Marks(ctx, change)
	if err != nil {
		return reviewedJSON{}, err
	}
	commits, err := svc.Commits(ctx, rev)
	if err != nil {
		return reviewedJSON{}, err
	}
	lists, err := svc.CommitFileLists(ctx, rev, commits)
	if err != nil {
		return reviewedJSON{}, err
	}
	diff, err := svc.RevisionFileList(ctx, rev)
	if err != nil {
		return reviewedJSON{}, err
	}
	out := reviewedJSON{
		Commits:   []commitReviewedJSON{},
		Diff:      []string{},
		DiffFiles: len(diff),
	}
	for _, c := range commits {
		files := lists[c.SHA]
		cj := commitReviewedJSON{
			SHA:      c.SHA,
			Subject:  c.Subject(),
			Reviewed: marks.CommitReviewed(c.SHA, files),
		}
		if !cj.Reviewed {
			for _, f := range files {
				if marks.FileReviewed(c.SHA, f) {
					cj.Files = append(cj.Files, f.Path())
				}
			}
		}
		out.Commits = append(out.Commits, cj)
	}
	for _, f := range diff {
		if marks[review.DiffFileMark(f)] {
			out.Diff = append(out.Diff, f.Path())
		}
	}
	return out, nil
}

// any reports whether anything is marked reviewed.
func (r reviewedJSON) any() bool {
	for _, c := range r.Commits {
		if c.Reviewed || len(c.Files) > 0 {
			return true
		}
	}
	return len(r.Diff) > 0
}

// printReviewed prints what is marked reviewed, one line per commit or file, after a count.
func (a *app) printReviewed(r reviewedJSON) {
	done := 0
	for _, c := range r.Commits {
		if c.Reviewed {
			done++
		}
	}
	fmt.Fprintf(a.stdout, "Reviewed by you: %d of %d commits, %d of %s of the whole diff\n",
		done, len(r.Commits), len(r.Diff), plural(r.DiffFiles, "file", "files"))
	for _, c := range r.Commits {
		if c.Reviewed {
			fmt.Fprintf(a.stdout, "  commit %s\n", short(c.SHA))
		}
		for _, p := range c.Files {
			fmt.Fprintf(a.stdout, "  commit %s %s\n", short(c.SHA), p)
		}
	}
	for _, p := range r.Diff {
		fmt.Fprintf(a.stdout, "  diff %s\n", p)
	}
}
