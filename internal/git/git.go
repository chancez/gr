// Package git runs the git binary and parses its output.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ErrShallow is returned by RootCommit for shallow clones, whose root commit is not available.
var ErrShallow = errors.New("shallow clones are not supported")

// ErrNoCommits is returned by RootCommit for a repository with no commits.
var ErrNoCommits = errors.New("repository has no commits")

// Repo is a git repository, addressed through one of its worktrees.
type Repo struct {
	// Dir is the top level of the worktree.
	Dir string
}

// Error is a failed git command.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Open finds the repository containing dir.
func Open(ctx context.Context, dir string) (*Repo, error) {
	out, err := run(ctx, dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	return &Repo{Dir: strings.TrimSpace(string(out))}, nil
}

// baseArgs are passed to every command so that user configuration cannot change the shape of the
// output gr parses.
var baseArgs = []string{
	"-c", "core.quotePath=false",
	"-c", "diff.noprefix=false",
	"-c", "diff.mnemonicPrefix=false",
	"-c", "color.ui=false",
}

func run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", slices.Concat(baseArgs, args)...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	slog.Debug("git", "args", args, "duration", time.Since(start), "bytes", stdout.Len(), "err", err)
	if err != nil {
		return nil, &Error{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

func (r *Repo) run(ctx context.Context, args ...string) ([]byte, error) {
	return run(ctx, r.Dir, nil, args...)
}

func (r *Repo) line(ctx context.Context, args ...string) (string, error) {
	out, err := r.run(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// exitCode reports the exit code of a failed git command, or -1 if it did not run.
func exitCode(err error) int {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	return -1
}

// RootCommit returns the repository's identity: the oldest root commit reachable from HEAD.
func (r *Repo) RootCommit(ctx context.Context) (string, error) {
	shallow, err := r.line(ctx, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return "", err
	}
	if shallow == "true" {
		return "", ErrShallow
	}
	if _, err := r.run(ctx, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		return "", ErrNoCommits
	}
	// Committer time, then sha, so the choice is stable when several roots share a timestamp.
	out, err := r.run(ctx, "log", "--max-parents=0", "--format=%ct %H", "HEAD")
	if err != nil {
		return "", err
	}
	var best string
	var bestTime int64
	for line := range strings.Lines(string(out)) {
		ts, sha, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		t, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return "", fmt.Errorf("parse root commit time %q: %w", ts, err)
		}
		if best == "" || t < bestTime || (t == bestTime && sha < best) {
			best, bestTime = sha, t
		}
	}
	if best == "" {
		return "", ErrNoCommits
	}
	return best, nil
}

// RepoName is the name of the repository at dir: its main worktree's directory, so a linked
// worktree is named for the repository rather than for itself.
func RepoName(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	common := strings.TrimSpace(string(out))
	if filepath.Base(common) == ".git" {
		common = filepath.Dir(common)
	}
	return strings.TrimSuffix(filepath.Base(common), ".git"), nil
}

// ResolveCommit resolves rev to a full commit sha.
func (r *Repo) ResolveCommit(ctx context.Context, rev string) (string, error) {
	return r.line(ctx, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
}

// Branches lists local branches, then remote branches as remote/name, without remotes' HEADs.
func (r *Repo) Branches(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, "for-each-ref", "--format=%(refname)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var names []string
	for ref := range strings.Lines(string(out)) {
		ref = strings.TrimSpace(ref)
		if strings.HasPrefix(ref, "refs/remotes/") && strings.HasSuffix(ref, "/HEAD") {
			continue
		}
		name, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok {
			name = strings.TrimPrefix(ref, "refs/remotes/")
		}
		names = append(names, name)
	}
	return names, nil
}

// StackParents lists the local branches branch is stacked on, nearest first: those whose commits
// branch contains, that have commits not yet on base or the branch base tracks, and that are not at
// branch's own commit. The tracked branch counts because a local base is often behind it, and
// branches that landed upstream since are not parents.
func (r *Repo) StackParents(ctx context.Context, branch, base string) ([]string, error) {
	args := []string{"for-each-ref", "--format=%(refname:short) %(objectname)",
		"--merged=refs/heads/" + branch, "--no-merged=" + base}
	if upstream, err := r.line(ctx, "rev-parse", "--verify", "--quiet", "--symbolic-full-name", base+"@{upstream}"); err == nil && upstream != "" {
		args = append(args, "--no-merged="+upstream)
	}
	out, err := r.run(ctx, append(args, "refs/heads")...)
	if err != nil {
		return nil, err
	}
	head, err := r.ResolveCommit(ctx, "refs/heads/"+branch)
	if err != nil {
		return nil, err
	}
	type parent struct {
		name     string
		distance int
	}
	var parents []parent
	for line := range strings.Lines(string(out)) {
		name, sha, _ := strings.Cut(strings.TrimSpace(line), " ")
		if name == branch || sha == head {
			continue
		}
		n, err := r.line(ctx, "rev-list", "--count", sha+".."+head)
		if err != nil {
			return nil, err
		}
		d, err := strconv.Atoi(n)
		if err != nil {
			return nil, err
		}
		parents = append(parents, parent{name, d})
	}
	slices.SortStableFunc(parents, func(a, b parent) int { return a.distance - b.distance })
	names := make([]string, len(parents))
	for i, p := range parents {
		names[i] = p.name
	}
	return names, nil
}

// BranchExists reports whether refs/heads/name exists.
func (r *Repo) BranchExists(ctx context.Context, name string) (bool, error) {
	_, err := r.run(ctx, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

// CurrentBranch returns the short name of the checked out branch. It fails on a detached HEAD.
func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	return r.line(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
}

// DefaultBranch guesses the branch changes are based on: the base remote's HEAD, or its branch
// named by init.defaultBranch, main, or master. Failing that, a local branch by one of those names.
// A remote branch is named by its local branch when one of that name exists, as "main" rather than
// "origin/main", unless the local branch is only behind it.
func (r *Repo) DefaultBranch(ctx context.Context) (string, error) {
	remote, err := r.baseRemote(ctx)
	if err != nil {
		return "", err
	}
	names, err := r.defaultNames(ctx)
	if err != nil {
		return "", err
	}
	ref := ""
	if remote != "" {
		if ref, err = r.line(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/"+remote+"/HEAD"); err != nil {
			ref = ""
			for _, name := range names {
				if r.refExists(ctx, "refs/remotes/"+remote+"/"+name) {
					ref = remote + "/" + name
					break
				}
			}
		}
	}
	if ref != "" {
		names = []string{strings.TrimPrefix(ref, remote+"/")}
	}
	for _, name := range names {
		exists, err := r.BranchExists(ctx, name)
		if err != nil {
			return "", err
		}
		if !exists {
			continue
		}
		// A local branch only behind the remote's would cut changes at a stale point, showing
		// commits that already landed. One with commits of its own is the one to use.
		if ref != "" {
			behind, err := r.strictlyBehind(ctx, "refs/heads/"+name, "refs/remotes/"+ref)
			if err != nil {
				return "", err
			}
			if behind {
				return ref, nil
			}
		}
		return name, nil
	}
	if ref != "" {
		return ref, nil
	}
	return "", errors.New("cannot determine the default branch; pass a base branch, or set git config checkout.defaultRemote")
}

// baseRemote picks the remote whose default branch changes are based on, or "" when there is none
// to pick. Git's settings decide first: checkout.defaultRemote, then clone.defaultRemoteName. Then
// origin, upstream, the only remote, or the one remote that is not remote.pushDefault.
func (r *Repo) baseRemote(ctx context.Context) (string, error) {
	out, err := r.run(ctx, "remote")
	if err != nil {
		return "", err
	}
	remotes := strings.Fields(string(out))
	if len(remotes) == 0 {
		return "", nil
	}
	var candidates []string
	for _, key := range []string{"checkout.defaultRemote", "clone.defaultRemoteName"} {
		name, err := r.Config(ctx, key)
		if err != nil {
			return "", err
		}
		candidates = append(candidates, name)
	}
	candidates = append(candidates, "origin", "upstream")
	for _, name := range candidates {
		if slices.Contains(remotes, name) {
			return name, nil
		}
	}
	if len(remotes) == 1 {
		return remotes[0], nil
	}
	push, err := r.Config(ctx, "remote.pushDefault")
	if err != nil {
		return "", err
	}
	if others := slices.DeleteFunc(slices.Clone(remotes), func(name string) bool { return name == push }); len(others) == 1 {
		return others[0], nil
	}
	return "", nil
}

// defaultNames are the names a default branch goes by: init.defaultBranch, then main and master.
func (r *Repo) defaultNames(ctx context.Context) ([]string, error) {
	names := []string{"main", "master"}
	name, err := r.Config(ctx, "init.defaultBranch")
	if err != nil {
		return nil, err
	}
	if name != "" && !slices.Contains(names, name) {
		names = append([]string{name}, names...)
	}
	return names, nil
}

// strictlyBehind reports whether a is an ancestor of b and not b itself.
func (r *Repo) strictlyBehind(ctx context.Context, a, b string) (bool, error) {
	shaA, err := r.ResolveCommit(ctx, a)
	if err != nil {
		return false, err
	}
	shaB, err := r.ResolveCommit(ctx, b)
	if err != nil {
		return false, err
	}
	if shaA == shaB {
		return false, nil
	}
	return r.IsAncestor(ctx, shaA, shaB)
}

func (r *Repo) refExists(ctx context.Context, ref string) bool {
	_, err := r.run(ctx, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// MergeBase returns the best common ancestor of a and b.
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	return r.line(ctx, "merge-base", a, b)
}

// IsAncestor reports whether a is an ancestor of, or equal to, b.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	_, err := r.run(ctx, "merge-base", "--is-ancestor", a, b)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

// Commit is one commit's metadata.
type Commit struct {
	SHA         string
	Parents     []string
	AuthorName  string
	AuthorEmail string
	AuthorTime  time.Time
	Message     string
}

// Subject is the first line of the message.
func (c Commit) Subject() string {
	subject, _, _ := strings.Cut(c.Message, "\n")
	return subject
}

const commitFormat = "%H%x00%P%x00%an%x00%ae%x00%at%x00%B"

func parseCommits(out []byte) ([]Commit, error) {
	var commits []Commit
	for record := range bytes.SplitSeq(out, []byte{0x1e}) {
		record = bytes.TrimLeft(record, "\n")
		if len(record) == 0 {
			continue
		}
		fields := strings.SplitN(string(record), "\x00", 6)
		if len(fields) != 6 {
			return nil, fmt.Errorf("malformed commit record %q", record)
		}
		ts, err := strconv.ParseInt(fields[4], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse author time %q: %w", fields[4], err)
		}
		commits = append(commits, Commit{
			SHA:         fields[0],
			Parents:     strings.Fields(fields[1]),
			AuthorName:  fields[2],
			AuthorEmail: fields[3],
			AuthorTime:  time.Unix(ts, 0),
			Message:     strings.TrimRight(fields[5], "\n") + "\n",
		})
	}
	return commits, nil
}

// Commits lists the commits in base..head, oldest first. An empty base lists all of head's history.
func (r *Repo) Commits(ctx context.Context, base, head string) ([]Commit, error) {
	rng := head
	if base != "" {
		rng = base + ".." + head
	}
	out, err := r.run(ctx, "log", "--reverse", "--format="+commitFormat+"%x1e", rng, "--")
	if err != nil {
		return nil, err
	}
	return parseCommits(out)
}

// ReadCommit reads one commit.
func (r *Repo) ReadCommit(ctx context.Context, sha string) (Commit, error) {
	out, err := r.run(ctx, "log", "-1", "--format="+commitFormat+"%x1e", sha, "--")
	if err != nil {
		return Commit{}, err
	}
	commits, err := parseCommits(out)
	if err != nil {
		return Commit{}, err
	}
	if len(commits) != 1 {
		return Commit{}, fmt.Errorf("commit %s not found", sha)
	}
	return commits[0], nil
}

// PatchIDs returns the stable patch id of each commit that changes something, by sha: commits
// with the same diff, as before and after a clean rebase, have the same one.
func (r *Repo) PatchIDs(ctx context.Context, shas []string) (map[string]string, error) {
	ids := map[string]string{}
	if len(shas) == 0 {
		return ids, nil
	}
	diffs, err := run(ctx, r.Dir, []byte(strings.Join(shas, "\n")+"\n"), "diff-tree", "--stdin", "-p", "--root", "--no-ext-diff")
	if err != nil {
		return nil, err
	}
	out, err := run(ctx, r.Dir, diffs, "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	for line := range strings.Lines(string(out)) {
		if id, sha, ok := strings.Cut(strings.TrimSpace(line), " "); ok {
			ids[sha] = id
		}
	}
	return ids, nil
}

// EmptyTree returns the id of the empty tree in this repository's hash algorithm.
func (r *Repo) EmptyTree(ctx context.Context) (string, error) {
	out, err := run(ctx, r.Dir, []byte{}, "hash-object", "-t", "tree", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ReadFile returns the contents of path at rev. ok is false if the path does not exist there.
func (r *Repo) ReadFile(ctx context.Context, rev, path string) (content []byte, ok bool, err error) {
	out, err := r.run(ctx, "cat-file", "blob", rev+":"+path)
	if err != nil {
		if exitCode(err) == 128 {
			return nil, false, nil
		}
		return nil, false, err
	}
	return out, true, nil
}

// BlobID returns the blob id of path at rev, or "" if it does not exist there.
func (r *Repo) BlobID(ctx context.Context, rev, path string) (string, error) {
	out, err := r.run(ctx, "rev-parse", "--verify", "--quiet", rev+":"+path)
	if err != nil {
		if exitCode(err) == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Diff returns the diff from one revision to another, limited to paths if any are given. An empty
// from diffs against the empty tree.
func (r *Repo) Diff(ctx context.Context, from, to string, paths ...string) ([]FileDiff, error) {
	if from == "" {
		var err error
		if from, err = r.EmptyTree(ctx); err != nil {
			return nil, err
		}
	}
	args := append([]string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-relative",
		"--full-index", "--src-prefix=a/", "--dst-prefix=b/", "-M", from, to, "--"}, paths...)
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return ParseDiff(out)
}

// CommitDiff returns the diff a commit introduces against its first parent.
func (r *Repo) CommitDiff(ctx context.Context, c Commit) ([]FileDiff, error) {
	parent := ""
	if len(c.Parents) > 0 {
		parent = c.Parents[0]
	}
	return r.Diff(ctx, parent, c.SHA)
}

// MergeTree merges theirs into ours with an explicit merge base, without touching the index or the
// worktree, and returns the resulting tree. clean is false if there were conflicts, in which case
// the tree contains conflict markers.
func (r *Repo) MergeTree(ctx context.Context, base, ours, theirs string) (tree string, clean bool, err error) {
	out, err := r.run(ctx, "merge-tree", "--write-tree", "--no-messages", "--merge-base="+base, ours, theirs)
	clean = err == nil
	if err != nil && exitCode(err) != 1 {
		return "", false, err
	}
	// The first line is the tree. Conflicted file info follows it.
	tree, _, _ = strings.Cut(string(out), "\n")
	if err != nil && tree == "" {
		return "", false, err
	}
	return strings.TrimSpace(tree), clean, nil
}

// Config returns a git config value, or "" if it is unset.
func (r *Repo) Config(ctx context.Context, key string) (string, error) {
	out, err := r.line(ctx, "config", "--get", key)
	if err != nil && exitCode(err) == 1 {
		return "", nil
	}
	return out, err
}
