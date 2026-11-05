package cli

import (
	"bytes"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/tui"
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

func TestChangeDelete(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Commit("one")
	human := "Human <h@example.com>"
	e.run(human, "review")
	e.run(human, "change", "abandon")
	e.run(human, "change", "delete", "1")
	if got := e.run(human, "change", "list", "--all"); got != "" {
		t.Errorf("change list after deleting = %q, want none", got)
	}
	e.run(human, "comment", "-m", "hm")
	e.run(human, "submit")
	if err := e.runErr(human, "change", "delete"); !strings.Contains(err.Error(), "force deletes them") {
		t.Errorf("deleting a reviewed change: %v, want it to need --force", err)
	}
	e.run(human, "change", "delete", "--force")
	if got := e.run(human, "change", "list", "--all"); got != "" {
		t.Errorf("change list after deleting with --force = %q, want none", got)
	}
}

// A new change on a stacked branch is based on its parent, and gr says so.
func TestStackedBranchNotice(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "parent")
	e.repo.Commit("parent")
	e.repo.Git("checkout", "-q", "-b", "child")
	e.repo.Commit("child")
	var stdout, stderr bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr, logPath: e.log}
	defer a.close()
	cmd := a.rootCommand()
	cmd.SetArgs([]string{"--db", e.db, "-C", e.repo.Dir, "--author", "Human", "review"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := "change #1 is based on parent, the branch it is stacked on; gr change base or B in the TUI moves it\n"
	if stderr.String() != want {
		t.Errorf("stderr = %q, want %q", stderr.String(), want)
	}
}

// Marks set from the CLI show in gr change show, as text and JSON.
func TestMarkAndUnmark(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Write("a.txt", gittest.Lines("a"), "b.txt", gittest.Lines("b"))
	root := e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Write("a.txt", gittest.Lines("A"), "b.txt", gittest.Lines("B"))
	both := e.repo.Commit("change both")
	e.repo.Write("a.txt", gittest.Lines("AA"))
	again := e.repo.Commit("change a again")
	agent := "agent"

	e.run(agent, "mark", "--commit", both)
	e.run(agent, "mark", "--commit", again, "-p", "a.txt", "-p", "/COMMIT_MSG")
	e.run(agent, "mark", "-p", "b.txt")
	// Unmarking a file of a marked commit leaves its other files marked.
	e.run(agent, "unmark", "--commit", both, "-p", "b.txt")
	var got reviewedJSON
	if err := json.Unmarshal([]byte(e.run(agent, "mark", "--json", "-p", "a.txt")), &got); err != nil {
		t.Fatal(err)
	}
	want := reviewedJSON{
		Commits: []commitReviewedJSON{
			{
				SHA:     both,
				Subject: "change both",
				Files:   []string{"/COMMIT_MSG", "a.txt"},
			},
			{
				SHA:      again,
				Subject:  "change a again",
				Reviewed: true,
			},
		},
		Diff:      []string{"a.txt", "b.txt"},
		DiffFiles: 2,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("mark --json mismatch (-want +got):\n%s", diff)
	}

	var show changeDetailJSON
	if err := json.Unmarshal([]byte(e.run(agent, "change", "show", "--json")), &show); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, show.Reviewed); diff != "" {
		t.Errorf("change show --json reviewed mismatch (-want +got):\n%s", diff)
	}

	e.run(agent, "unmark", "-p", "a.txt")
	wantText := "Change #1: feature\n" +
		"open, feature onto main, revision 1 (" + short(root) + ".." + short(again) + ")\n" +
		"Threads: 0 open, 0 addressed, 0 resolved\n" +
		"Reviewed by you: 1 of 2 commits, 1 of 2 files of the whole diff\n" +
		"  commit " + short(both) + " /COMMIT_MSG\n" +
		"  commit " + short(both) + " a.txt\n" +
		"  commit " + short(again) + "\n" +
		"  diff b.txt\n"
	if got := e.run(agent, "change", "show"); got != wantText {
		t.Errorf("change show = %q, want %q", got, wantText)
	}
	// Marks are the author's own.
	if got := e.run("Human", "change", "show"); strings.Contains(got, "Reviewed") {
		t.Errorf("change show for another author = %q, want no marks", got)
	}

	for args, want := range map[string]string{
		"mark":                      "pass --commit, -p, or both",
		"mark -p missing.txt":       "missing.txt is not in the change's diff",
		"mark --commit " + root:     "is not in revision 1",
		"unmark --commit HEAD -p x": "x is not in commit " + short(again),
	} {
		if err := e.runErr(agent, strings.Fields(args)...); !strings.Contains(err.Error(), want) {
			t.Errorf("gr %s: %v, want %q", args, err, want)
		}
	}
}

// The TUI's settings come from the config file, with GR_THEME and GR_GLYPHS over it.
func TestTUIOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[ui]\ntheme = \"colorblind\"\nglyphs = \"ascii\"\nscrolloff = 2\nsidebar_width = 28\nauto_advance = true\n[editor]\nvim = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GR_CONFIG", path)
	t.Setenv("GR_THEME", "")
	t.Setenv("GR_GLYPHS", "nerd")
	got, err := tuiOptions()
	if err != nil {
		t.Fatal(err)
	}
	want := tui.Options{Theme: "colorblind", Glyphs: "nerd", Scrolloff: new(2), SidebarWidth: 28, AutoAdvance: true, Vim: true}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("tuiOptions (-want +got):\n%s", diff)
	}
}

// Under an agent, a command that writes as its author needs one given, rather than writing as the
// human's git identity; reading needs none.
func TestAgentsNeedAnAuthor(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Write("f.txt", "one\n")
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Write("f.txt", "two\n")
	e.repo.Commit("change")
	// The human's identity, which an agent with no author would write as.
	e.repo.Git("config", "user.name", "Human")
	e.repo.Git("config", "user.email", "h@example.com")
	run := func(args ...string) error {
		var stdout, stderr bytes.Buffer
		a := &app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr, logPath: e.log,
			getenv: func(k string) string { return map[string]string{"CLAUDECODE": "1"}[k] }}
		cmd := a.rootCommand()
		cmd.SetArgs(append([]string{"--db", e.db, "-C", e.repo.Dir}, args...))
		return cmd.ExecuteContext(t.Context())
	}
	if err := run("comment", "-m", "as whom?"); err == nil || !strings.Contains(err.Error(), "set GR_AUTHOR") {
		t.Errorf("comment under an agent with no author: %v, want it refused", err)
	}
	if err := run("threads"); err != nil {
		t.Errorf("threads under an agent with no author: %v, want it read", err)
	}
	if err := run("--author", "Claude", "comment", "-m", "as Claude"); err != nil {
		t.Errorf("comment under an agent with an author: %v", err)
	}
}

func TestVersion(t *testing.T) {
	e := newCLIEnv(t)
	if got := e.run("Human", "--version"); got != "gr version dev\n" {
		t.Errorf("gr --version = %q", got)
	}

	tagged := &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}}
	devel := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	got := []string{
		versionOf("v0.3.0", tagged, true),
		versionOf("", tagged, true),
		versionOf("", devel, true),
		versionOf("", nil, false),
	}
	if diff := cmp.Diff([]string{"v0.3.0", "v0.2.0", "dev", "dev"}, got); diff != "" {
		t.Errorf("versions (-want +got):\n%s", diff)
	}
}

func TestChangeBranch(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "poc")
	e.repo.Commit("one")
	human := "Human <h@example.com>"
	e.run(human, "comment", "-m", "a start")
	e.run(human, "submit", "-m", "")
	// Made again under a new name rather than renamed, so git has no record to follow.
	e.repo.Git("checkout", "-q", "-b", "feature")
	e.repo.Git("branch", "-D", "poc")
	e.run(human, "review")
	if got, want := e.run(human, "change", "list"), "#2\topen\tfeature onto main\tfeature\n#1\topen\tpoc onto main\tpoc\n"; got != want {
		t.Errorf("after the new branch, change list = %q, want %q", got, want)
	}
	e.run(human, "change", "branch", "1")
	if got, want := e.run(human, "change", "list"), "#1\topen\tfeature onto main\tfeature\n"; got != want {
		t.Errorf("after change branch 1, change list = %q, want %q", got, want)
	}
}

func TestFollowRenamedBranch(t *testing.T) {
	e := newCLIEnv(t)
	e.repo.Commit("root")
	e.repo.Git("checkout", "-q", "-b", "poc")
	e.repo.Commit("one")
	human := "Human <h@example.com>"
	e.run(human, "comment", "-m", "a start")
	e.run(human, "submit", "-m", "")
	e.repo.Git("branch", "-m", "poc", "feature")
	e.run(human, "review")
	if got, want := e.run(human, "change", "list"), "#1\topen\tfeature onto main\tfeature\n"; got != want {
		t.Errorf("after renaming poc to feature, change list = %q, want %q", got, want)
	}
	var shown changeDetailJSON
	if err := json.Unmarshal([]byte(e.run(human, "change", "show", "1", "--json")), &shown); err != nil {
		t.Fatal(err)
	}
	want := []moveJSON{{Kind: "branch", From: "poc", To: "feature", How: "renamed", Author: authorJSON{Name: "Human", Email: "h@example.com"}}}
	if diff := cmp.Diff(want, shown.Moves, cmpopts.IgnoreFields(moveJSON{}, "CreatedAt")); diff != "" {
		t.Errorf("change show --json moves (-want +got):\n%s", diff)
	}
	text := e.run(human, "change", "show", "1")
	if _, moves, _ := strings.Cut(text, "\nMoves:\n"); !strings.HasSuffix(moves, " branch poc -> feature, following a rename, by Human <h@example.com>\n") {
		t.Errorf("change show moves = %q", moves)
	}

	// The name it had still finds it, as a change, a target, or --change.
	for _, args := range [][]string{{"change", "show", "poc"}, {"review", "poc"}, {"change", "show", "--change", "poc"}} {
		if got, _, _ := strings.Cut(e.run(human, args...), "\n"); got != "Change #1: feature" {
			t.Errorf("gr %s = %q, want change #1", strings.Join(args, " "), got)
		}
	}

	// A copy is a branch of its own until moved: the change stays on the branch copied.
	e.repo.Git("branch", "-c", "feature", "copy")
	e.repo.Git("checkout", "-q", "copy")
	e.run(human, "review")
	if got, want := e.run(human, "change", "list"), "#2\topen\tcopy onto main\tcopy\n#1\topen\tfeature onto main\tfeature\n"; got != want {
		t.Errorf("after copying, change list = %q, want %q", got, want)
	}
}
