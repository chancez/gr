// Package cli is gr's command line interface.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// app holds global flags and the lazily opened service.
type app struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	dbPath string
	author string
	dir    string
	json   bool

	store *store.Store
	svc   *review.Service
}

// Execute runs gr with os.Args and returns the exit code.
func Execute(ctx context.Context) int {
	a := &app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	cmd := a.rootCommand()
	cmd.SetArgs(os.Args[1:])
	err := cmd.ExecuteContext(ctx)
	a.close()
	if err != nil {
		fmt.Fprintln(a.stderr, "gr:", err)
		return 1
	}
	return 0
}

func (a *app) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "gr",
		Short: "Local code review for git",
		Long: `gr reviews branches, commit ranges, and single commits locally, with GitHub-style
reviews: threads on the change, a commit, a file, or lines, batched into a review with a verdict.

Commands that act on a change take --change to pick one by number or id prefix. Without it, they
use the change for the current branch, creating it if needed.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(a.stdin)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	flags := root.PersistentFlags()
	flags.StringVar(&a.dbPath, "db", os.Getenv("GR_DB"), "database path (default $XDG_DATA_HOME/gr/gr.db, env GR_DB)")
	flags.StringVar(&a.author, "author", os.Getenv("GR_AUTHOR"), `act as "Name <email>" or a bare name (default git config user.name, env GR_AUTHOR)`)
	flags.StringVarP(&a.dir, "repo", "C", ".", "run as if started in this directory")
	flags.BoolVar(&a.json, "json", false, "print JSON")

	root.AddCommand(
		a.reviewCommand(),
		a.changesCommand(),
		a.changeCommand(),
		a.threadsCommand(),
		a.threadCommand(),
		a.commentCommand(),
		a.replyCommand(),
		a.stateCommand("resolve", store.ThreadResolved, "Mark a thread resolved"),
		a.stateCommand("address", store.ThreadAddressed, "Mark a thread addressed, for a reviewer to confirm"),
		a.stateCommand("reopen", store.ThreadOpen, "Reopen a thread"),
		a.editCommand(),
		a.deleteCommand(),
		a.submitCommand(),
		a.discardCommand(),
	)
	return root
}

func (a *app) close() {
	if a.store != nil {
		a.store.Close()
	}
}

// service opens the database and repository on first use.
func (a *app) service(ctx context.Context) (*review.Service, error) {
	if a.svc != nil {
		return a.svc, nil
	}
	g, err := git.Open(ctx, a.dir)
	if err != nil {
		return nil, err
	}
	path := a.dbPath
	if path == "" {
		if path, err = store.DefaultPath(); err != nil {
			return nil, err
		}
	}
	st, err := store.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	a.store = st
	author, err := a.resolveAuthor(ctx, g)
	if err != nil {
		return nil, err
	}
	if a.svc, err = review.Open(ctx, st, g, author); err != nil {
		return nil, err
	}
	return a.svc, nil
}

func (a *app) resolveAuthor(ctx context.Context, g *git.Repo) (store.Author, error) {
	if a.author != "" {
		return review.ParseAuthor(a.author), nil
	}
	name, err := g.Config(ctx, "user.name")
	if err != nil {
		return store.Author{}, err
	}
	email, err := g.Config(ctx, "user.email")
	if err != nil {
		return store.Author{}, err
	}
	return store.Author{Name: name, Email: email}, nil
}

// changeFlags selects a change: an explicit --change, else the current branch.
type changeFlags struct {
	change string
	base   string
}

func (f *changeFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.change, "change", "c", "", "change number or id prefix (default: the current branch's change)")
	cmd.Flags().StringVar(&f.base, "base", "", "base branch when creating a change (default: the default branch)")
}

func (a *app) resolveChange(ctx context.Context, f changeFlags, spec string) (*review.Service, store.Change, store.Revision, error) {
	svc, err := a.service(ctx)
	if err != nil {
		return nil, store.Change{}, store.Revision{}, err
	}
	if f.change != "" {
		if spec != "" {
			return nil, store.Change{}, store.Revision{}, fmt.Errorf("pass a target or --change, not both")
		}
		c, rev, err := svc.ChangeByRef(ctx, f.change)
		return svc, c, rev, err
	}
	target, err := svc.ParseTarget(ctx, spec, f.base)
	if err != nil {
		return nil, store.Change{}, store.Revision{}, err
	}
	c, rev, err := svc.EnsureChange(ctx, target)
	return svc, c, rev, err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
