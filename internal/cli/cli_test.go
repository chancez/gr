package cli

import (
	"bytes"
	"encoding/json/v2"
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
}

func newCLIEnv(t *testing.T) *cliEnv {
	return &cliEnv{t: t, repo: gittest.New(t), db: filepath.Join(t.TempDir(), "gr.db")}
}

// run runs gr as author and returns stdout.
func (e *cliEnv) run(author string, args ...string) string {
	e.t.Helper()
	var stdout, stderr bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr}
	defer a.close()
	cmd := a.rootCommand()
	cmd.SetArgs(append([]string{"--db", e.db, "-C", e.repo.Dir, "--author", author}, args...))
	if err := cmd.ExecuteContext(e.t.Context()); err != nil {
		e.t.Fatalf("gr %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
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
	e.run(agent, "resolve", "2")

	var got []threadJSON
	if err := json.Unmarshal([]byte(e.run(agent, "threads", "--json", "--unresolved")), &got); err != nil {
		t.Fatal(err)
	}
	want := []threadJSON{{
		Seq: 1, Change: 1, State: "addressed", StateBy: &authorJSON{Name: "agent"},
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
	if diff := cmp.Diff(want, byCommit, ignore); diff != "" {
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
