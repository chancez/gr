// Package gittest builds throwaway git repositories for tests.
package gittest

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Repo is a git repository in a temp directory.
type Repo struct {
	t   testing.TB
	Dir string
	// commits makes commit timestamps deterministic and increasing.
	commits int
}

// Epoch is the timestamp of the first commit. Each later commit is one second after the previous.
var Epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// New creates an empty repository with main as its initial branch.
func New(t testing.TB) *Repo {
	t.Helper()
	// Resolve symlinks so Dir matches what git rev-parse --show-toplevel reports on macOS.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &Repo{t: t, Dir: dir}
	r.Git("init", "-q", "-b", "main")
	return r
}

// Git runs git in the repository and returns its trimmed stdout.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	return r.gitEnv(nil, args...)
}

func (r *Repo) gitEnv(env []string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.Output()
	if err != nil {
		var stderr []byte
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			stderr = exitErr.Stderr
		}
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(string(out))
}

// Write writes files, given as path and content pairs, relative to the repository root.
func (r *Repo) Write(pathContent ...string) {
	r.t.Helper()
	if len(pathContent)%2 != 0 {
		r.t.Fatal("Write needs path and content pairs")
	}
	for i := 0; i < len(pathContent); i += 2 {
		path := filepath.Join(r.Dir, pathContent[i])
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(pathContent[i+1]), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
}

// Commit stages everything and commits it, returning the new sha.
func (r *Repo) Commit(message string) string {
	r.t.Helper()
	date := Epoch.Add(time.Duration(r.commits) * time.Second).Format(time.RFC3339)
	r.commits++
	r.Git("add", "-A")
	r.gitEnv([]string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date},
		"commit", "-q", "--allow-empty", "--no-verify", "-m", message)
	return r.Git("rev-parse", "HEAD")
}

// CommitTime is the author and committer time of the nth commit made through Commit, from 0.
func CommitTime(n int) time.Time {
	return Epoch.Add(time.Duration(n) * time.Second)
}

// Lines joins lines with a trailing newline, for writing file contents.
func Lines(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// Numbered returns n lines "line 1" through "line n".
func Numbered(n int) []string {
	lines := make([]string, n)
	for i := range n {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return lines
}
