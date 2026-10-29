// Package git runs the git binary and parses its output.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
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
	if err := cmd.Run(); err != nil {
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

// ResolveCommit resolves rev to a full commit sha.
func (r *Repo) ResolveCommit(ctx context.Context, rev string) (string, error) {
	return r.line(ctx, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
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

// DefaultBranch guesses the branch changes are based on: origin's HEAD, then main, then master.
func (r *Repo) DefaultBranch(ctx context.Context) (string, error) {
	if ref, err := r.line(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name, ok := strings.CutPrefix(ref, "origin/"); ok {
			if exists, err := r.BranchExists(ctx, name); err == nil && exists {
				return name, nil
			}
		}
		return ref, nil
	}
	for _, name := range []string{"main", "master"} {
		exists, err := r.BranchExists(ctx, name)
		if err != nil {
			return "", err
		}
		if exists {
			return name, nil
		}
	}
	return "", errors.New("cannot determine the default branch; pass a base branch")
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

// Diff returns the diff from one revision to another. An empty from diffs against the empty tree.
func (r *Repo) Diff(ctx context.Context, from, to string) ([]FileDiff, error) {
	if from == "" {
		var err error
		if from, err = r.EmptyTree(ctx); err != nil {
			return nil, err
		}
	}
	out, err := r.run(ctx, "diff", "--no-ext-diff", "--no-textconv", "--no-color", "--no-relative",
		"--src-prefix=a/", "--dst-prefix=b/", "-M", from, to, "--")
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
