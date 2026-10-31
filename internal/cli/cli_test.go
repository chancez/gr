package cli

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/chancez/gr/internal/gittest"
)

type cliEnv struct {
	t    *testing.T
	repo *gittest.Repo
	db   string
	log  string
}

func newCLIEnv(t *testing.T) *cliEnv {
	dir := t.TempDir()
	return &cliEnv{t: t, repo: gittest.New(t), db: filepath.Join(dir, "gr.db"), log: filepath.Join(dir, "gr.log")}
}

// run runs gr as author and returns stdout.
func (e *cliEnv) run(author string, args ...string) string {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr, logPath: e.log}
	defer a.close()
	cmd := a.rootCommand()
	cmd.SetArgs(append([]string{"--db", e.db, "-C", e.repo.Dir, "--author", author}, args...))
	if err := cmd.ExecuteContext(e.t.Context()); err != nil {
		e.t.Fatalf("gr %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}

// runErr runs gr as author and returns its error, failing the test if it succeeds.
func (e *cliEnv) runErr(author string, args ...string) error {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr, logPath: e.log}
	defer a.close()
	cmd := a.rootCommand()
	cmd.SetArgs(append([]string{"--db", e.db, "-C", e.repo.Dir, "--author", author}, args...))
	err := cmd.ExecuteContext(e.t.Context())
	if err == nil {
		e.t.Fatalf("gr %s succeeded, want an error", strings.Join(args, " "))
	}
	return err
}

func TestReviewRoundTrip(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Write("f.txt", gittest.Lines("a", "b", "c"))
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Write("f.txt", gittest.Lines("a", "B", "c"))
	commit := e.repo.Commit("upper b")

	human, agent := "Human <h@example.com>", "agent"
	e.run(human, "comment", "--commit", commit, "-p", "f.txt", "-l", "2", "-m", "why?")
	e.run(human, "comment", "-m", "overall")
	if got := e.run(agent, "threads", "--json"); strings.TrimSpace(got) != "[]" {
		t.Errorf("agent saw draft threads: %s", got)
	}
	e.run(human, "submit", "--request-changes", "-m", "fix it")
	e.run(agent, "address", "1", "-m", "done")
	e.run(agent, "reply", "r1", "-m", "on it")
	e.run(agent, "resolve", "2")

	var got []threadJSON
	if err := json.Unmarshal([]byte(e.run(agent, "threads", "--json", "--unresolved")), &got); err != nil {
		t.Fatal(err)
	}
	want := []threadJSON{{
		// The review's summary, open for a reply, numbered by its review.
		Ref: "r1", Change: 1, Review: 1, State: "open", Summary: true,
		Location: locationJSON{Scope: "change", Revision: 1},
		Comments: []commentJSON{
			{Author: authorJSON{Name: "Human", Email: "h@example.com"}, Body: "fix it"},
			{Author: authorJSON{Name: "agent"}, Body: "on it"},
		},
	}, {
		Ref: "1", Seq: 1, Change: 1, Review: 1, State: "addressed", StateBy: &authorJSON{Name: "agent"},
		Location: locationJSON{
			Scope: "commit", Revision: 1, Commit: commit, Path: "f.txt", Side: "new", StartLine: 2, EndLine: 2, Lines: []string{"B"},
		},
		Context: &excerptJSON{
			Path: "f.txt", Side: "new", Hunk: "@@ -1,3 +1,3 @@", Exact: true,
			Lines: []lineJSON{
				{Kind: "context", Old: 1, New: 1, Text: "a"},
				{Kind: "deleted", Old: 2, Text: "b"},
				{Kind: "added", New: 2, Text: "B"},
			},
		},
		Comments: []commentJSON{
			{Author: authorJSON{Name: "Human", Email: "h@example.com"}, Body: "why?"},
			{Author: authorJSON{Name: "agent"}, Body: "done"},
		},
	}}
	ignore := cmp.Options{
		cmpopts.IgnoreFields(threadJSON{}, "ID"),
		cmpopts.IgnoreFields(commentJSON{}, "ID", "CreatedAt", "UpdatedAt"),
	}
	if diff := cmp.Diff(want, got, ignore); diff != "" {
		t.Errorf("threads mismatch (-want +got):\n%s", diff)
	}

	var byCommit []threadJSON
	if err := json.Unmarshal([]byte(e.run(agent, "threads", "--json", "--commit", commit)), &byCommit); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want[1:], byCommit, ignore); diff != "" {
		t.Errorf("threads --commit mismatch (-want +got):\n%s", diff)
	}
}

func TestParseLines(t *testing.T) {
	for in, want := range map[string][2]int{"5": {5, 5}, "3-7": {3, 7}} {
		start, end, err := parseLines(in)
		if err != nil || [2]int{start, end} != want {
			t.Errorf("parseLines(%q) = %d, %d, %v; want %v", in, start, end, err, want)
		}
	}
	for _, in := range []string{"", "0", "5-3", "x", "1-"} {
		if _, _, err := parseLines(in); err == nil {
			t.Errorf("parseLines(%q) succeeded, want error", in)
		}
	}
}

func TestDeleteCommands(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	human := "Human <h@example.com>"
	e.run(human, "comment", "-m", "first")
	e.run(human, "submit", "-m", "summary")
	e.run(human, "comment", "-m", "second")
	e.run(human, "submit", "--request-changes")

	e.run(human, "delete", "--thread", "1")
	e.run(human, "discard", "--review", "2")
	var got []threadJSON
	if err := json.Unmarshal([]byte(e.run(human, "threads", "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Ref != "r1" {
		t.Errorf("threads after deleting #1 and review 2 = %+v, want only r1", got)
	}
}

func TestChangeListAndMerged(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	human := "Human <h@example.com>"
	e.run(human, "review")
	if got := e.run(human, "change", "list"); !strings.Contains(got, "#1\topen") {
		t.Errorf("change list = %q", got)
	}
	e.run(human, "change", "merged", "--change", "1")
	if got := e.run(human, "change", "list"); strings.Contains(got, "#1") {
		t.Errorf("change list after merged = %q, want only open changes", got)
	}
	if got := e.run(human, "change", "list", "--all"); !strings.Contains(got, "#1\tmerged") {
		t.Errorf("change list --all = %q", got)
	}
}

func TestDebugLogsGitCommands(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	e.run("Human <h@example.com>", "--debug", "threads")
	b, err := os.ReadFile(e.log)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); !strings.Contains(got, "msg=git") || !strings.Contains(got, "duration=") {
		t.Errorf("debug log lacks git commands:\n%s", got)
	}
}

func TestControlCharactersAreShown(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Write("f.txt", gittest.Lines("a"))
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Write("f.txt", gittest.Lines("a \x1b[5mblink"))
	commit := e.repo.Commit("blink")
	e.run("Human", "comment", "--commit", commit, "-p", "f.txt", "-l", "1", "-m", "title \x1b]0;pwned\x07")
	e.run("Human", "submit")
	for _, args := range [][]string{{"threads"}, {"thread", "1"}, {"change", "show"}} {
		got := e.run("agent", args...)
		if strings.ContainsAny(got, "\x1b\x07") {
			t.Errorf("gr %s sent control characters: %q", strings.Join(args, " "), got)
		}
	}
	if got, want := e.run("agent", "threads"), "^[[5mblink"; !strings.Contains(got, want) {
		t.Errorf("gr threads does not show %q:\n%s", want, got)
	}
}

// The change commands act on an existing change, picked by number or by its branch, and never
// create one.
func TestChangeCommandsFindAChange(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	human := "Human <h@example.com>"
	e.run(human, "review")
	list := func() string { return e.run(human, "change", "list", "--all") }

	e.run(human, "change", "abandon")
	err := e.runErr(human, "change", "abandon")
	if want := "no open change for branch feature"; !strings.Contains(err.Error(), want) {
		t.Errorf("abandoning again: %v, want %q", err, want)
	}
	if got, want := list(), "#1\tabandoned\tfeature onto main\tfeature\n"; got != want {
		t.Errorf("after abandoning twice, change list = %q, want %q", got, want)
	}
	// With no open change, reopen takes the branch's latest.
	e.run(human, "change", "reopen")
	for _, step := range [][]string{
		{"change", "abandon", "1"},
		{"change", "reopen", "#1"},
		{"change", "merged", "feature"},
		{"change", "reopen", "--change", "1"},
	} {
		e.run(human, step...)
	}
	if got, want := list(), "#1\topen\tfeature onto main\tfeature\n"; got != want {
		t.Errorf("after reopening by number and branch, change list = %q, want %q", got, want)
	}
	if got := e.run(human, "change", "show", "1"); !strings.HasPrefix(got, "Change #1: feature\n") {
		t.Errorf("change show 1 = %q", got)
	}
}

// A change's base can be moved, by gr change base or by passing a different --base, and stays the
// same change.
func TestChangeBase(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "parent")
	e.repo.Commit("parent")
	e.repo.Git("checkout", "-q", "-b", "child")
	e.repo.Commit("child")
	human := "Human <h@example.com>"
	list := func() string { return e.run(human, "change", "list") }
	e.run(human, "review")
	e.run(human, "review", "--base", "parent")
	if got, want := list(), "#1\topen\tchild onto parent\tchild\n"; got != want {
		t.Errorf("after review --base parent, change list = %q, want %q", got, want)
	}
	e.run(human, "change", "base", "main")
	if got, want := list(), "#1\topen\tchild onto main\tchild\n"; got != want {
		t.Errorf("after change base main, change list = %q, want %q", got, want)
	}
	e.run(human, "change", "base", "parent", "1")
	if got, want := list(), "#1\topen\tchild onto parent\tchild\n"; got != want {
		t.Errorf("after change base parent 1, change list = %q, want %q", got, want)
	}
}
