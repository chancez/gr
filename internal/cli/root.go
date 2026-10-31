// Package cli is gr's command line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/logging"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/termtext"
	"github.com/chancez/gr/internal/tui"
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
	debug  bool
	// logPath overrides the log file's location, for tests.
	logPath string
	logFile io.Closer

	store *store.Store
	svc   *review.Service
}

// Execute runs gr with os.Args and returns the exit code.
func Execute(ctx context.Context) int {
	a := &app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	cmd := a.rootCommand()
	cmd.SetArgs(os.Args[1:])
	err := cmd.ExecuteContext(ctx)
	if err != nil {
		slog.Warn("command failed", "args", os.Args[1:], "err", err)
	}
	a.close()
	if err != nil {
		fmt.Fprintln(a.stderr, "gr:", err)
		return 1
	}
	return 0
}

func (a *app) rootCommand() *cobra.Command {
	// Names, paths, and comments can hold control characters, which a terminal would act on.
	a.stdout, a.stderr = termtext.NewWriter(a.stdout), termtext.NewWriter(a.stderr)
	root := &cobra.Command{
		Short: "Local code review for git",
		Use:   "gr [target]",
		Long: `gr reviews branches, commit ranges, and single commits locally, with GitHub-style
reviews: threads on the change, a commit, a file, or lines, batched into a review with a verdict.

Run without a command to review in the TUI. A target is a branch (default: the current one), a
range A..B, or a commit. GR_THEME=colorblind uses blue and orange in place of green and red.

Logs go to $XDG_STATE_HOME/gr/gr.log, by default ~/.local/state/gr/gr.log.

Commands that act on a change take --change to pick one by number or id prefix. Without it, they
use the change for the current branch. Reviewing and commenting create it if needed; the gr change
commands only find one, and take a change number such as 4 directly.`,
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	var cf changeFlags
	cf.register(root)
	root.RunE = func(cmd *cobra.Command, args []string) error {
		svc, change, _, err := a.resolveChange(cmd.Context(), cf, firstArg(args))
		if err != nil {
			return err
		}
		return tui.Run(cmd.Context(), svc, change, tui.Options{Theme: os.Getenv("GR_THEME")})
	}
	root.SetIn(a.stdin)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	flags := root.PersistentFlags()
	flags.StringVar(&a.dbPath, "db", os.Getenv("GR_DB"), "database path (default $XDG_DATA_HOME/gr/gr.db, env GR_DB)")
	flags.StringVar(&a.author, "author", os.Getenv("GR_AUTHOR"), `act as "Name <email>" or a bare name (default git config user.name, env GR_AUTHOR)`)
	flags.StringVarP(&a.dir, "repo", "C", ".", "run as if started in this directory")
	flags.BoolVar(&a.json, "json", false, "print JSON")
	flags.BoolVar(&a.debug, "debug", false, "log at debug level, as GR_LOG=debug does")
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error { return a.setupLogging() }

	root.AddCommand(
		a.reviewCommand(),
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

// setupLogging sends logs to the log file, at the level from --debug or GR_LOG. A log file that
// cannot be opened is reported once and logging is turned off, rather than failing the command.
func (a *app) setupLogging() error {
	level, err := logging.ParseLevel(os.Getenv("GR_LOG"))
	if err != nil {
		return err
	}
	if a.debug {
		level = slog.LevelDebug
	}
	path := a.logPath
	if path == "" {
		if path, err = logging.DefaultPath(); err != nil {
			return err
		}
	}
	logger, f, err := logging.Open(path, level)
	if err != nil {
		fmt.Fprintf(a.stderr, "gr: not logging: %v\n", err)
		logger = logging.Discard()
	}
	a.logFile = f
	slog.SetDefault(logger)
	slog.Debug("start", "args", os.Args[1:])
	return nil
}

func (a *app) close() {
	if a.logFile != nil {
		a.logFile.Close()
	}
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
	cmd.Flags().StringVar(&f.base, "base", "", "base branch; moves an existing change onto it (default: the default branch)")
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
		if err != nil {
			return nil, store.Change{}, store.Revision{}, err
		}
		return a.moveBase(ctx, svc, c, rev, f.base)
	}
	target, err := svc.ParseTarget(ctx, spec, f.base)
	if err != nil {
		return nil, store.Change{}, store.Revision{}, err
	}
	c, rev, err := svc.EnsureChange(ctx, target)
	if err != nil {
		return nil, store.Change{}, store.Revision{}, err
	}
	return a.moveBase(ctx, svc, c, rev, f.base)
}

// moveBase moves a branch change onto base when --base names a different one, so passing --base
// again fixes a change created against the wrong base.
func (a *app) moveBase(ctx context.Context, svc *review.Service, c store.Change, rev store.Revision, base string) (*review.Service, store.Change, store.Revision, error) {
	if base == "" || c.Kind != store.KindBranch || c.BaseBranch == base {
		return svc, c, rev, nil
	}
	from := c.BaseBranch
	c, rev, err := svc.SetBase(ctx, c, base)
	if err != nil {
		return nil, store.Change{}, store.Revision{}, err
	}
	fmt.Fprintf(a.stderr, "change #%d moved from %s onto %s\n", c.Seq, from, base)
	return svc, c, rev, nil
}

// changeRef picks an existing change, for the commands that act on one.
type changeRef struct {
	change string
}

func (f *changeRef) register(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&f.change, "change", "c", "", "change number or id prefix (default: the current branch's change)")
}

// changeArgHelp explains the argument of the commands that act on an existing change.
const changeArgHelp = `The change is a number such as 4 or #4, a branch, a range A..B, or a commit, and defaults to the
current branch's change. These commands never create a change.`

// findChange finds an existing change: by --change, by an argument that is a change number such as
// 4 or #4, or as a target's open change. It never creates one. With latest, a branch with no open
// change gives its newest change instead, for reopening it.
func (a *app) findChange(ctx context.Context, f changeRef, arg string, latest bool) (*review.Service, store.Change, store.Revision, error) {
	svc, err := a.service(ctx)
	if err != nil {
		return nil, store.Change{}, store.Revision{}, err
	}
	fail := func(err error) (*review.Service, store.Change, store.Revision, error) {
		return nil, store.Change{}, store.Revision{}, err
	}
	switch {
	case f.change != "" && arg != "":
		return fail(errors.New("pass a change or --change, not both"))
	case f.change != "":
		arg = f.change
		fallthrough
	case isChangeNumber(arg):
		c, rev, err := svc.ChangeByRef(ctx, arg)
		if errors.Is(err, store.ErrNotFound) {
			err = fmt.Errorf("no change %s; gr change list --all lists every change", arg)
		}
		return svc, c, rev, err
	}
	branch := arg
	if branch == "" {
		if branch, err = svc.Git().CurrentBranch(ctx); err != nil {
			return fail(errors.New("HEAD is not on a branch; pass a change number"))
		}
	}
	target := review.Target{Kind: store.KindBranch, Branch: branch}
	isBranch, err := svc.Git().BranchExists(ctx, branch)
	if err != nil {
		return fail(err)
	}
	if !isBranch {
		if target, err = svc.ParseTarget(ctx, arg, ""); err != nil {
			return fail(err)
		}
	}
	c, rev, err := svc.FindChange(ctx, target)
	if errors.Is(err, store.ErrNotFound) && latest && target.Kind == store.KindBranch {
		c, rev, err = svc.LatestBranchChange(ctx, branch)
	}
	if errors.Is(err, store.ErrNotFound) {
		what := "branch " + branch
		if target.Kind != store.KindBranch {
			what = arg
		}
		err = fmt.Errorf("no open change for %s; gr change list --all lists every change", what)
	}
	return svc, c, rev, err
}

// isChangeNumber reports whether s names a change by number, as 4 or #4.
func isChangeNumber(s string) bool {
	_, err := strconv.Atoi(strings.TrimPrefix(s, "#"))
	return err == nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
