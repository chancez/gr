// Package cli is gr's command line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/chancez/gr/internal/config"
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
	// getenv reads the environment, which tests replace.
	getenv  func(string) string
	logFile io.Closer

	store *store.Store
	svc   *review.Service
	// notice is something gr decided for the user, shown when the TUI opens.
	notice string
	// rename is the change of the branch the one opened was copied from, for the TUI to offer.
	rename *review.Rename
}

// Execute runs gr with os.Args and returns the exit code.
func Execute(ctx context.Context) int {
	a := &app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, getenv: os.Getenv}
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
	info, ok := debug.ReadBuildInfo()
	root := &cobra.Command{
		Short: "Local code review for git",
		Use:   "gr [target]",
		Long: `gr reviews branches, commit ranges, and single commits locally, with GitHub-style
reviews: threads on the change, a commit, a file, or lines, batched into a review with a verdict.

Run without a command to review in the TUI. A target is a branch (default: the current one), a
range A..B, or a commit. GR_THEME=colorblind uses blue and orange in place of green and red.
GR_GLYPHS=ascii, unicode, or nerd sets the symbols drawn with; by default Nerd Font icons are used
in terminals that bundle them, such as kitty, WezTerm, and Ghostty, and Unicode elsewhere. Both can
also be set in the config file, $XDG_CONFIG_HOME/gr/config.toml or GR_CONFIG.

Logs go to $XDG_STATE_HOME/gr/gr.log, by default ~/.local/state/gr/gr.log.

Commands that act on a change take --change to pick one by number or id prefix. Without it, they
use the change for the current branch. Reviewing and commenting create it if needed; the gr change
commands only find one, and take a change number such as 4 directly.`,
		Args:          cobra.MaximumNArgs(1),
		Version:       versionOf(version, info, ok),
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
		opts, err := tuiOptions()
		if err != nil {
			return err
		}
		opts.Notice, opts.Rename = a.notice, a.rename
		// Another repository's changes open in tabs through a service of their own, on the same
		// database and as the same author.
		opts.OpenRepo = func(ctx context.Context, dir string) (*review.Service, error) {
			g, err := git.Open(ctx, dir)
			if err != nil {
				return nil, err
			}
			return review.Open(ctx, a.store, g, svc.Author())
		}
		return tui.Run(cmd.Context(), svc, change, opts)
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
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if err := a.setupLogging(); err != nil {
			return err
		}
		return a.checkAgentAuthor(cmd)
	}

	root.AddCommand(a.reviewCommand(), a.changeCommand(), a.threadsCommand(), a.threadCommand())
	// What these write is the author's: comments, thread states, marks, and reviews.
	for _, cmd := range []*cobra.Command{
		a.commentCommand(),
		a.replyCommand(),
		a.stateCommand("resolve", store.ThreadResolved, "Mark a thread resolved"),
		a.stateCommand("address", store.ThreadAddressed, "Mark a thread addressed, for a reviewer to confirm"),
		a.stateCommand("reopen", store.ThreadOpen, "Reopen a thread"),
		a.markCommand(true),
		a.markCommand(false),
		a.editCommand(),
		a.deleteCommand(),
		a.submitCommand(),
		a.discardCommand(),
	} {
		cmd.Annotations = map[string]string{writesAsAuthor: "true"}
		root.AddCommand(cmd)
	}
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

// writesAsAuthor annotates a command that writes as its author.
const writesAsAuthor = "gr/writes-as-author"

// agentEnv are environment variables set by coding agents for the commands they run.
var agentEnv = []string{"CLAUDECODE"}

// checkAgentAuthor refuses a command that writes as its author when an agent runs it with no author
// given: gr would write as the git identity, which is the human's, and the agent's comments would
// read as theirs. Reading is left alone.
func (a *app) checkAgentAuthor(cmd *cobra.Command) error {
	if a.author != "" || cmd.Annotations[writesAsAuthor] == "" {
		return nil
	}
	getenv := a.getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	for _, v := range agentEnv {
		if getenv(v) != "" {
			return fmt.Errorf("an agent is running gr (%s is set), and gr %s would write as your git identity; "+
				"set GR_AUTHOR to the agent's name, or pass --author with whose it is", v, cmd.Name())
		}
	}
	return nil
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
		// A branch's old name finds the change that moved off it.
		c, rev, oldErr := svc.ChangeByOldBranch(ctx, spec)
		if spec == "" || errors.Is(oldErr, store.ErrNotFound) {
			return nil, store.Change{}, store.Revision{}, err
		}
		if oldErr != nil {
			return nil, store.Change{}, store.Revision{}, oldErr
		}
		return a.moveBase(ctx, svc, c, rev, f.base)
	}
	if target.Kind == store.KindBranch {
		if target, err = a.followRename(ctx, svc, target, spec, f.base); err != nil {
			return nil, store.Change{}, store.Revision{}, err
		}
	}
	c, rev, err := svc.EnsureChange(ctx, target)
	if err != nil {
		return nil, store.Change{}, store.Revision{}, err
	}
	if target.Stacked {
		a.notice = fmt.Sprintf("change #%d is based on %s, the branch it is stacked on; gr change base or B in the TUI moves it", c.Seq, c.BaseBranch)
		fmt.Fprintln(a.stderr, a.notice)
	}
	return a.moveBase(ctx, svc, c, rev, f.base)
}

// followRename moves the change of the branch's earlier name onto it, when git recorded a rename,
// and says so. A branch copied from one with a change is only told about, for the TUI to offer
// and gr change branch to move. A moved change keeps its base, so the target is read again.
func (a *app) followRename(ctx context.Context, svc *review.Service, target review.Target, spec, base string) (review.Target, error) {
	r, ok, err := svc.FollowRename(ctx, target.Branch)
	if err != nil || !ok {
		return target, err
	}
	if r.Kept {
		a.rename = &r
		a.notice = fmt.Sprintf("%s was copied from %s, which change #%d follows; gr change branch %d moves it here",
			target.Branch, r.From, r.Change.Seq, r.Change.Seq)
		fmt.Fprintln(a.stderr, a.notice)
		return target, nil
	}
	a.notice = fmt.Sprintf("change #%d follows %s, renamed from %s", r.Change.Seq, target.Branch, r.From)
	fmt.Fprintln(a.stderr, a.notice)
	return svc.ParseTarget(ctx, spec, base)
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
		// A branch's old name finds the change that moved off it.
		if c, rev, err := svc.ChangeByOldBranch(ctx, branch); err == nil || !errors.Is(err, store.ErrNotFound) {
			return svc, c, rev, err
		}
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

// tuiOptions are the TUI's settings: the config file's, under GR_THEME and GR_GLYPHS.
func tuiOptions() (tui.Options, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return tui.Options{}, err
	}
	c, err := config.Load(path)
	if err != nil {
		return tui.Options{}, err
	}
	opts := tui.Options{
		Theme:        c.UI.Theme,
		Glyphs:       c.UI.Glyphs,
		Scrolloff:    c.UI.Scrolloff,
		SidebarWidth: c.UI.SidebarWidth,
		AutoAdvance:  c.UI.AutoAdvance,
		Vim:          c.Editor.Vim,
	}
	if v := os.Getenv("GR_THEME"); v != "" {
		opts.Theme = v
	}
	if v := os.Getenv("GR_GLYPHS"); v != "" {
		opts.Glyphs = v
	}
	return opts, nil
}
