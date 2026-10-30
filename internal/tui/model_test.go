package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

type tuiEnv struct {
	t     *testing.T
	repo  *gittest.Repo
	svc   *review.Service
	m     *model
	chg   store.Change
	store *store.Store
}

func newTUIEnv(t *testing.T) *tuiEnv {
	t.Helper()
	r := gittest.New(t)
	r.Write("main.go", gittest.Lines("package main", "", "func main() {", "\tprintln(\"hi\")", "}"))
	r.Commit("root")
	r.Git("checkout", "-q", "-b", "feature")
	r.Write("main.go", gittest.Lines("package main", "", "func main() {", "\tprintln(\"hello\")", "}"))
	r.Commit("say hello\n\nLonger explanation.")

	st, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "gr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	g, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := review.Open(t.Context(), st, g, store.Author{Name: "Human", Email: "h@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	target, err := svc.ParseTarget(t.Context(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	chg, _, err := svc.EnsureChange(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	m, err := newModel(t.Context(), svc, chg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.close)
	e := &tuiEnv{t: t, repo: r, svc: svc, m: m, chg: chg, store: st}
	e.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	e.drain(m.loadCmd())
	return e
}

// drain runs a command that is known to finish quickly and feeds its message back, following the
// chain of data loads it starts.
func (e *tuiEnv) drain(cmd tea.Cmd) {
	e.t.Helper()
	for cmd != nil {
		msg := cmd()
		switch msg.(type) {
		case dataMsg, opMsg:
			_, cmd = e.m.Update(msg)
		default:
			return
		}
	}
}

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	if rest, ok := strings.CutPrefix(s, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	r := rune(s[0])
	return tea.KeyPressMsg{Code: r, Text: s}
}

// press sends keys, ignoring the commands they return.
func (e *tuiEnv) press(keys ...string) {
	for _, k := range keys {
		e.m.Update(keyMsg(k))
	}
}

// pressRun sends one key and drains its command.
func (e *tuiEnv) pressRun(k string) {
	e.t.Helper()
	_, cmd := e.m.Update(keyMsg(k))
	e.drain(cmd)
}

func (e *tuiEnv) screen() string { return ansi.Strip(e.m.render()) }

func (e *tuiEnv) typeText(s string) {
	for _, r := range s {
		e.m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// openNav opens the view whose sidebar label contains s.
func (e *tuiEnv) openNav(s string) {
	e.t.Helper()
	for i, it := range e.m.viewsNav.items {
		if strings.Contains(it.label, s) {
			e.m.viewsNav.pos = i
			e.m.openView()
			return
		}
	}
	e.t.Fatalf("no nav item %q", s)
}

func (e *tuiEnv) cursorTo(match func(row) bool) {
	e.t.Helper()
	for i, r := range e.m.main.rows {
		if match(r) {
			e.m.main.cursor = i
			e.m.main.ensureVisible()
			return
		}
	}
	e.t.Fatal("no matching row")
}

func addedLine(r row) bool { return r.kind == rowLine && r.line != nil && r.line.Kind == git.LineAdded }

func TestOverview(t *testing.T) {
	e := newTUIEnv(t)
	screen := e.screen()
	for _, want := range []string{"#1 feature", "Overview", "All changes", "say h", "Threads: 0 open"} {
		if !strings.Contains(screen, want) {
			t.Errorf("overview missing %q:\n%s", want, screen)
		}
	}
}

func TestCommentOnLineAndSubmit(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
	e.press("c")
	if e.m.mode != modeCompose || !strings.Contains(e.m.compose.title, "main.go:4") {
		t.Fatalf("composer not open on main.go:4: mode %d title %q", e.m.mode, e.m.compose.title)
	}
	e.typeText("Why the change?")
	e.pressRun("ctrl+s")

	threads, err := e.svc.Threads(t.Context(), e.chg, review.ThreadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || threads[0].Comments[0].Body != "Why the change?" || threads[0].Location.StartLine != 4 {
		t.Fatalf("threads after commenting = %+v", threads)
	}
	if screen := e.screen(); !strings.Contains(screen, "Why the change?") || !strings.Contains(screen, "draft") {
		t.Errorf("thread not shown inline as a draft:\n%s", screen)
	}

	e.press("S")
	if e.m.mode != modeVerdict {
		t.Fatalf("mode after S = %d, want verdict", e.m.mode)
	}
	e.press("r")
	e.typeText("Needs work")
	e.pressRun("ctrl+s")
	reviews, err := e.svc.Reviews(t.Context(), e.chg)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 1 || reviews[0].State != store.ReviewSubmitted || reviews[0].Verdict != store.VerdictRequestChanges || reviews[0].Body != "Needs work" {
		t.Errorf("reviews after submit = %+v", reviews)
	}
}

func TestRangeSelection(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("All changes")
	e.cursorTo(func(r row) bool { return r.kind == rowLine && r.line != nil && r.line.NewLine == 3 })
	e.press("v", "j", "j", "j", "c")
	if !strings.Contains(e.m.compose.title, "main.go:3-5") {
		t.Errorf("range comment title = %q, want main.go:3-5", e.m.compose.title)
	}
}

func TestCommentOnCommitMessageAndThreadActions(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 0 })
	e.press("c")
	e.typeText("subject nit")
	e.pressRun("ctrl+s")

	e.cursorTo(func(r row) bool { return r.kind == rowThread })
	e.press("r")
	e.typeText("reply")
	e.pressRun("ctrl+s")
	e.cursorTo(func(r row) bool { return r.kind == rowThread })
	e.pressRun("x")

	threads, err := e.svc.Threads(t.Context(), e.chg, review.ThreadFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads = %+v", threads)
	}
	got := threads[0]
	if got.Location.Path != review.CommitMsgPath || got.State != store.ThreadResolved || len(got.Comments) != 2 {
		t.Errorf("thread = %+v", got)
	}
}

func TestSplitView(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("All changes")
	e.press("s")
	screen := e.screen()
	if !strings.Contains(screen, `println("hi")`) || !strings.Contains(screen, `println("hello")`) {
		t.Fatalf("split view missing a side:\n%s", screen)
	}
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, `println("hi")`) && !strings.Contains(line, `println("hello")`) {
			t.Errorf("old and new lines are not side by side: %q", line)
		}
	}
}

func TestLiveRefresh(t *testing.T) {
	e := newTUIEnv(t)
	// Another process comments.
	other, err := review.Open(t.Context(), e.store, e.svc.Git(), store.Author{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.StartThread(t.Context(), e.chg, review.NewThread{Scope: store.ScopeChange, Body: "from the agent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Submit(t.Context(), e.chg, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	msg := e.m.refreshCmd()().(refreshMsg)
	if !msg.changed {
		t.Fatal("refresh did not notice another connection's write")
	}
	_, cmd := e.m.Update(msg)
	e.drain(cmd)
	if !strings.Contains(e.screen(), "from the agent") {
		t.Errorf("refreshed overview missing the agent's comment:\n%s", e.screen())
	}

	// A new commit on the branch is noticed too.
	e.repo.Write("new.go", "package main\n")
	e.repo.Commit("add file")
	msg = e.m.refreshCmd()().(refreshMsg)
	if !msg.changed {
		t.Fatal("refresh did not notice the branch moving")
	}
	_, cmd = e.m.Update(msg)
	e.drain(cmd)
	if e.m.data.revision.Seq != 2 {
		t.Errorf("revision after the branch moved = %d, want 2", e.m.data.revision.Seq)
	}
}

func TestPairLines(t *testing.T) {
	lines := []git.Line{
		{Kind: git.LineContext, Text: "a"},
		{Kind: git.LineDeleted, Text: "b"},
		{Kind: git.LineDeleted, Text: "c"},
		{Kind: git.LineAdded, Text: "B"},
		{Kind: git.LineContext, Text: "d"},
		{Kind: git.LineAdded, Text: "e"},
	}
	var got [][2]string
	for _, p := range pairLines(lines) {
		var pair [2]string
		for i, l := range p {
			if l != nil {
				pair[i] = l.Text
			}
		}
		got = append(got, pair)
	}
	want := [][2]string{{"a", "a"}, {"b", "B"}, {"c", ""}, {"d", "d"}, {"", "e"}}
	if len(got) != len(want) {
		t.Fatalf("pairLines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pairLines = %v, want %v", got, want)
			break
		}
	}
}

func TestFocusMovesBetweenPanes(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	if e.m.focus != focusMain {
		t.Fatalf("initial focus = %d, want main", e.m.focus)
	}
	// h goes to the file list by default, and h again stays in the sidebar.
	e.press("h", "h")
	if e.m.focus != focusFiles {
		t.Errorf("focus after h h from main = %d, want files", e.m.focus)
	}
	e.press("ctrl+k")
	if e.m.focus != focusViews {
		t.Errorf("focus after ctrl+k = %d, want the change list", e.m.focus)
	}
	e.press("l")
	if e.m.focus != focusMain {
		t.Errorf("focus after l from the change list = %d, want main", e.m.focus)
	}
	// h returns to the section used last.
	e.press("h")
	if e.m.focus != focusViews {
		t.Errorf("focus after h = %d, want the change list again", e.m.focus)
	}
	e.press("ctrl+j", "l")
	if e.m.focus != focusMain {
		t.Errorf("focus after ctrl+j l = %d, want main", e.m.focus)
	}

	// In split mode, h goes to the old side before leaving the pane.
	e.press("s", "l", "h")
	if e.m.focus != focusMain || e.m.main.side != "old" {
		t.Errorf("after h in split: focus %d side %s, want main and old", e.m.focus, e.m.main.side)
	}
	e.press("h")
	if e.m.focus != focusFiles {
		t.Errorf("focus after h on the old side = %d, want files", e.m.focus)
	}
}

func TestPaneHeaders(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	lines := strings.Split(e.screen(), "\n")
	if !strings.Contains(lines[0], "Change #1") || !strings.Contains(lines[0], "say hello") {
		t.Errorf("first line lacks the sidebar and main headers: %q", lines[0])
	}
	if !strings.Contains(e.screen(), " Files") {
		t.Errorf("files section header missing:\n%s", e.screen())
	}
}

func TestSidebarOrderAndFileList(t *testing.T) {
	e := newTUIEnv(t)
	var labels []string
	for _, it := range e.m.viewsNav.items {
		labels = append(labels, it.label)
	}
	want := []string{"Overview", "say hello", "All changes"}
	if len(labels) != len(want) {
		t.Fatalf("views = %q, want %q", labels, want)
	}
	for i := range want {
		if !strings.Contains(labels[i], want[i]) {
			t.Errorf("views = %q, want %q", labels, want)
		}
	}
	e.openNav("say hello")
	if got := len(e.m.filesNav.items); got != 2 {
		t.Errorf("files for the commit = %d, want 2 (message and main.go)", got)
	}
}

func TestDragSidebar(t *testing.T) {
	e := newTUIEnv(t)
	nw, _, _ := e.m.layoutSizes()
	e.m.Update(tea.MouseClickMsg{X: nw, Y: 3, Button: tea.MouseLeft})
	e.m.Update(tea.MouseMotionMsg{X: nw + 10, Y: 3, Button: tea.MouseLeft})
	e.m.Update(tea.MouseReleaseMsg{X: nw + 10, Y: 3, Button: tea.MouseLeft})
	if got, _, _ := e.m.layoutSizes(); got != nw+10 {
		t.Errorf("sidebar width after drag = %d, want %d", got, nw+10)
	}
}

func TestSidebarTruncates(t *testing.T) {
	l := navList{items: []navItem{{label: "a very long label that does not fit"}}}
	got := ansi.Strip(l.render(newStyles(true, ThemeDefault), "T", 12, 2, false)[1])
	if got != "a very lo..." {
		t.Errorf("truncated label = %q", got)
	}
}

func TestFileLabelsKeepTheirEnd(t *testing.T) {
	l := navList{items: []navItem{{prefix: "M ", label: "internal/anchor/anchor_test.go"}}}
	got := ansi.Strip(l.render(newStyles(true, ThemeDefault), "T", 20, 2, false)[1])
	if got != "M .../anchor_test.go" {
		t.Errorf("file label = %q", got)
	}
}

func TestCommitAndHunkNavigation(t *testing.T) {
	e := newTUIEnv(t)
	lines := gittest.Numbered(30)
	e.repo.Write("long.txt", gittest.Lines(lines...))
	e.repo.Commit("add long file")
	lines[1], lines[27] = "changed 2", "changed 28"
	e.repo.Write("long.txt", gittest.Lines(lines...))
	second := e.repo.Commit("edit both ends")
	e.drain(e.m.loadCmd())

	e.press("}")
	if v := e.m.views[e.m.current]; v.kind != viewCommit || v.commit.Subject() != "say hello" {
		t.Fatalf("} from the overview opened %q", e.m.viewLabel(v))
	}
	e.press("}", "}", "}")
	if v := e.m.views[e.m.current]; v.commit.SHA != second {
		t.Fatalf("} past the last commit opened %q, want the last commit", e.m.viewLabel(v))
	}
	e.press("{")
	if v := e.m.views[e.m.current]; v.commit.Subject() != "add long file" {
		t.Errorf("{ opened %q", e.m.viewLabel(v))
	}
	e.press("}")

	e.cursorTo(func(r row) bool { return r.kind == rowFile && r.file == 1 })
	var hunks []int
	for range 3 {
		e.press("J")
		if r := e.m.main.rows[e.m.main.cursor]; r.kind == rowHunk {
			hunks = append(hunks, r.hunk.NewStart)
		}
	}
	if len(hunks) != 3 || hunks[0] == hunks[1] || hunks[1] != hunks[2] {
		t.Errorf("hunk starts visited by J = %v, want two distinct hunks then a stop", hunks)
	}
	e.press("K")
	if r := e.m.main.rows[e.m.main.cursor]; r.kind != rowHunk || r.hunk.NewStart != hunks[0] {
		t.Errorf("K did not go back to the first hunk")
	}
}

func TestMarkReviewedAdvances(t *testing.T) {
	e := newTUIEnv(t)
	e.repo.Write("other.go", "package main\n")
	e.repo.Commit("add other")
	e.drain(e.m.loadCmd())
	e.openNav("say hello")

	// Mark the message, then main.go: the commit is done and the next one opens.
	e.cursorTo(func(r row) bool { return r.kind == rowFile && r.file == 0 })
	e.pressRun("m")
	if r := e.m.main.rows[e.m.main.cursor]; r.kind != rowFile || r.file != 1 {
		t.Fatalf("after marking the message, cursor on %+v, want main.go's header", r)
	}
	if e.m.main.rows[0].kind != rowCommit || !e.m.main.rows[1].collapsed {
		t.Errorf("reviewed file is not collapsed")
	}
	e.pressRun("m")
	if v := e.m.views[e.m.current]; v.kind != viewCommit || v.commit.Subject() != "add other" {
		t.Fatalf("after reviewing every file, view is %q, want the next commit", e.m.viewLabel(v))
	}
	reviewedItem := false
	for _, it := range e.m.viewsNav.items {
		if strings.Contains(it.label, "say hello") {
			reviewedItem = it.badge == markReviewed
		}
	}
	if !reviewedItem || !strings.Contains(e.screen(), "1/2 commits reviewed") {
		t.Errorf("commit not shown as reviewed:\n%s", e.screen())
	}

	// M reviews the whole commit.
	e.pressRun("M")
	if !strings.Contains(e.screen(), "2/2 commits reviewed") {
		t.Errorf("M did not review the commit:\n%s", e.screen())
	}

	// Marks persist.
	marks, err := e.svc.Marks(t.Context(), e.chg)
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) != 3 {
		t.Errorf("stored marks = %v, want the two files and one commit", marks)
	}

	// Unmarking one file of a fully reviewed commit keeps the others.
	e.openNav("say hello")
	e.cursorTo(func(r row) bool { return r.kind == rowFile && r.file == 1 })
	e.pressRun("m")
	v := e.m.views[e.m.current]
	if e.m.fileReviewed(v, 1) || !e.m.fileReviewed(v, 0) {
		t.Errorf("after unmarking main.go: message reviewed %v, main.go reviewed %v", e.m.fileReviewed(v, 0), e.m.fileReviewed(v, 1))
	}
}

func TestExpandReviewedFile(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("All changes")
	e.cursorTo(func(r row) bool { return r.kind == rowFile })
	e.pressRun("m")
	e.cursorTo(func(r row) bool { return r.kind == rowFile })
	if len(e.m.main.rows) != 1 {
		t.Fatalf("collapsed view has %d rows, want only the file header", len(e.m.main.rows))
	}
	e.press("enter")
	if len(e.m.main.rows) == 1 {
		t.Error("enter did not expand the reviewed file")
	}
}

func TestAllChangesToggle(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.press("A")
	if v := e.m.views[e.m.current]; v.kind != viewDiff {
		t.Fatalf("A opened %q, want all changes", e.m.viewLabel(v))
	}
	e.press("A")
	if v := e.m.views[e.m.current]; v.kind != viewCommit {
		t.Errorf("second A opened %q, want the commit again", e.m.viewLabel(v))
	}
}

func TestSidebarDividerAndClicks(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	lines := strings.Split(e.screen(), "\n")
	vh, _ := e.m.sidebarHeights(e.m.height - 1)
	if !strings.HasPrefix(lines[vh], "----") || !strings.Contains(lines[vh+1], "Files") {
		t.Errorf("no divider before the files section:\n%s", e.screen())
	}
	// Clicking the second file selects it.
	e.m.Update(tea.MouseClickMsg{X: 2, Y: vh + 3, Button: tea.MouseLeft})
	if e.m.focus != focusFiles || e.m.filesNav.pos != 1 {
		t.Errorf("click on the second file: focus %d pos %d", e.m.focus, e.m.filesNav.pos)
	}
}

func TestActiveHunkMarker(t *testing.T) {
	e := newTUIEnv(t)
	lines := gittest.Numbered(30)
	e.repo.Write("long.txt", gittest.Lines(lines...))
	e.repo.Commit("add long file")
	lines[1], lines[27] = "changed 2", "changed 28"
	e.repo.Write("long.txt", gittest.Lines(lines...))
	e.repo.Commit("edit both ends")
	e.drain(e.m.loadCmd())
	e.openNav("edit both ends")
	e.cursorTo(func(r row) bool { return r.kind == rowFile && r.file == 1 })
	e.press("J", "J")

	screen := e.screen()
	if !strings.Contains(screen, "(hunk 2/2)") {
		t.Errorf("title lacks the hunk position:\n%s", screen)
	}
	// The bar marks the second hunk's lines only: lines 25 to 30.
	var marked []string
	e.m.main.visible(func(i int, state rowState) []string {
		if state.activeHunk {
			r := e.m.main.rows[i]
			text := "@@"
			if r.line != nil {
				text = r.line.Text
			}
			marked = append(marked, text)
		}
		return e.m.renderMainRow(i, state)
	})
	if len(marked) != 8 || !strings.HasPrefix(marked[0], "@@") || !strings.Contains(marked[len(marked)-1], "line 30") {
		t.Errorf("marked rows = %q, want the second hunk's header and its 7 lines", marked)
	}
}

func TestChangeNavigation(t *testing.T) {
	e := newTUIEnv(t)
	lines := gittest.Numbered(12)
	e.repo.Write("long.txt", gittest.Lines(lines...))
	e.repo.Commit("add long file")
	// Two runs of changes in one hunk: line 3, and lines 6 and 7.
	lines[2], lines[5], lines[6] = "changed 3", "changed 6", "changed 7"
	e.repo.Write("long.txt", gittest.Lines(lines...))
	e.repo.Commit("edit")
	e.drain(e.m.loadCmd())
	e.openNav("edit")
	e.cursorTo(func(r row) bool { return r.kind == rowFile && r.file == 1 })

	var starts []string
	for range 3 {
		e.press(")")
		r := e.m.main.rows[e.m.main.cursor]
		starts = append(starts, r.line.Text)
	}
	want := []string{"line 3", "line 6", "line 6"}
	if strings.Join(starts, ",") != strings.Join(want, ",") {
		t.Errorf(") visited %q, want %q", starts, want)
	}
	e.press("(")
	if r := e.m.main.rows[e.m.main.cursor]; r.line.Text != "line 3" {
		t.Errorf("( went to %q, want line 3", r.line.Text)
	}
}

func TestSearch(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("All changes")
	e.press("/")
	if e.m.mode != modeSearch {
		t.Fatalf("mode after / = %d, want search", e.m.mode)
	}
	e.typeText("PRINTLN")
	e.press("enter")
	if !strings.Contains(e.m.status, "no match") {
		t.Errorf("uppercase pattern matched case-insensitively: %q", e.m.status)
	}
	e.press("/")
	e.typeText("println")
	e.press("enter")
	first := e.m.main.cursor
	if r := e.m.main.rows[first]; r.line == nil || !strings.Contains(r.line.Text, "println") {
		t.Fatalf("cursor after search on %+v", r)
	}
	if !strings.Contains(ansi.Strip(e.m.renderStatus()), "/println 1/2 esc clears") {
		t.Errorf("status bar = %q, want the search and its position", ansi.Strip(e.m.renderStatus()))
	}
	e.press("n")
	second := e.m.main.cursor
	e.press("n")
	if second == first || e.m.main.cursor != first || !strings.Contains(e.m.status, "wrapped") {
		t.Errorf("n did not visit both matches and wrap: %d %d %d %q", first, second, e.m.main.cursor, e.m.status)
	}
	e.press("N")
	if e.m.main.cursor != second {
		t.Errorf("N went to %d, want %d", e.m.main.cursor, second)
	}
}

func TestGaps(t *testing.T) {
	f := git.FileDiff{OldPath: "f", NewPath: "f", Hunks: []git.Hunk{
		{OldStart: 5, OldLines: 3, NewStart: 5, NewLines: 4},
		// Deletes old lines 20 and 21, after new line 20.
		{OldStart: 20, OldLines: 2, NewStart: 20, NewLines: 0},
	}}
	got := gaps(f)
	want := []gap{
		{index: 0, start: 1, end: 4, offset: 0},
		{index: 1, start: 9, end: 20, offset: -1},
		{index: 2, start: 21, offset: 1},
	}
	if len(got) != len(want) {
		t.Fatalf("gaps = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("gaps = %+v, want %+v", got, want)
		}
	}
	if gaps(git.FileDiff{NewPath: "added", Hunks: f.Hunks}) != nil {
		t.Error("an added file has gaps")
	}
}

func TestFolds(t *testing.T) {
	e := newTUIEnv(t)
	lines := gittest.Numbered(30)
	e.repo.Write("long.txt", gittest.Lines(lines...))
	e.repo.Commit("add long file")
	lines[1], lines[27] = "changed 2", "changed 28"
	e.repo.Write("long.txt", gittest.Lines(lines...))
	e.repo.Commit("edit both ends")
	e.drain(e.m.loadCmd())
	e.openNav("edit both ends")

	// Lines 6 to 24 sit between the hunks, and there is nothing after line 30.
	if !strings.Contains(e.screen(), "... 19 unchanged lines") {
		t.Fatalf("no fold between hunks:\n%s", e.screen())
	}
	e.cursorTo(func(r row) bool { return r.kind == rowGap && r.gap.index == 1 })
	e.press("z", "o")
	if r := e.m.main.rows[e.m.main.cursor]; r.kind != rowLine || r.line.Text != "line 6" || r.line.OldLine != 6 {
		t.Fatalf("after zo, cursor on %+v, want line 6", r)
	}
	if strings.Contains(e.screen(), "19 unchanged") {
		t.Error("fold still shown after zo")
	}
	e.press("c")
	if !strings.Contains(e.m.status, "cannot be commented") {
		t.Errorf("commenting on folded context: status %q", e.m.status)
	}
	e.press("j", "z", "c")
	if r := e.m.main.rows[e.m.main.cursor]; r.kind != rowGap {
		t.Errorf("after zc, cursor on %+v, want the closed fold", r)
	}

	// The trailing fold opens to nothing when the last hunk reaches the end of the file.
	e.press("z", "R")
	for _, r := range e.m.main.rows {
		if r.kind == rowGap {
			t.Errorf("fold left closed after zR: %+v", *r.gap)
		}
	}
	e.press("z", "M")
	for _, r := range e.m.main.rows {
		if r.kind == rowLine {
			t.Fatalf("line shown after zM: %+v", r)
		}
	}
	e.press("z", "R")
	if !strings.Contains(e.screen(), "line 15") {
		t.Errorf("zR did not open the file and its folds:\n%s", e.screen())
	}
}

func TestSidebarMarks(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
	e.press("c")
	e.typeText("draft")
	e.pressRun("ctrl+s")

	for _, it := range e.m.viewsNav.items {
		if strings.Contains(it.label, "say hello") && it.badge != markDraft {
			t.Errorf("commit badge %q, want the draft mark", it.badge)
		}
	}
	for _, it := range e.m.filesNav.items {
		if strings.Contains(it.label, "main.go") && it.badge != "(1) "+markDraft {
			t.Errorf("main.go badge %q, want the open thread count and the draft mark", it.badge)
		}
		if strings.Contains(it.label, "COMMIT_MSG") && it.badge != "" {
			t.Errorf("message badge %q, want none", it.badge)
		}
	}
	if marks(true, true) != "*x" || marks(false, false) != "" {
		t.Errorf("marks = %q and %q", marks(true, true), marks(false, false))
	}

	// A badge survives truncation.
	it := navItem{label: "a very long commit subject", badge: "(2) *x"}
	if got := ansi.Strip(it.render(newStyles(true, ThemeDefault), 20, false, false)); got != "a very lon... (2) *x" {
		t.Errorf("rendered = %q", got)
	}
}

func TestStatusExpires(t *testing.T) {
	e := newTUIEnv(t)
	e.m.setStatus("submitted")
	e.m.expireStatus(e.m.statusAt.Add(statusTTL - time.Second))
	if e.m.status != "submitted" {
		t.Fatal("status cleared early")
	}
	header := ansi.Strip(strings.Split(e.screen(), "\n")[0])
	if !strings.HasSuffix(header, " submitted ") || !strings.Contains(ansi.Strip(e.m.renderStatus()), "q quit") {
		t.Fatalf("status not in the title bar beside intact hints: header %q, bar %q", header, ansi.Strip(e.m.renderStatus()))
	}
	e.m.expireStatus(e.m.statusAt.Add(statusTTL))
	if e.m.status != "" || !strings.Contains(ansi.Strip(e.m.renderStatus()), "q quit") {
		t.Errorf("after expiry status = %q, bar = %q", e.m.status, ansi.Strip(e.m.renderStatus()))
	}

	e.m.setError(errors.New("boom"))
	e.m.expireStatus(e.m.statusAt.Add(statusTTL))
	if e.m.status != "boom" {
		t.Error("error expired as fast as a status")
	}
	e.m.expireStatus(e.m.statusAt.Add(errorTTL))
	if e.m.status != "" {
		t.Error("error never expired")
	}
}

func TestKeyHintsFit(t *testing.T) {
	e := newTUIEnv(t)
	for _, width := range []int{120, 80} {
		e.m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
		bar := strings.TrimRight(ansi.Strip(e.m.renderStatus()), " ")
		if !strings.HasSuffix(bar, "? help | q quit") {
			t.Errorf("width %d: help and quit not kept: %q", width, bar)
		}
	}
	e.m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if bar := ansi.Strip(e.m.renderStatus()); !strings.Contains(bar, "? help | q quit") {
		t.Errorf("width 140: hints = %q", bar)
	}
}

func TestConversationView(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	commit := e.m.data.commits[0].SHA
	line, err := e.svc.StartThread(ctx, e.chg, review.NewThread{
		Scope: store.ScopeCommit, Commit: commit, Path: "main.go", StartLine: 4, Body: "why hello?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictRequestChanges, "Two things."); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetThreadState(ctx, line.ID, store.ThreadAddressed); err != nil {
		t.Fatal(err)
	}
	e.drain(e.m.loadCmd())
	e.openNav("Overview")

	screen := e.screen()
	for _, want := range []string{"requested changes", "Two things.", "Summary (r1)", "why hello?", `+     println("hello")`, "commit "} {
		if !strings.Contains(screen, want) {
			t.Errorf("conversation missing %q:\n%s", want, screen)
		}
	}
	// The summary leads its review.
	if strings.Index(screen, "Two things.") > strings.Index(screen, "why hello?") {
		t.Error("summary is not first in its review")
	}

	// F cycles the filter: all, unresolved, open, addressed.
	e.press("F", "F")
	if screen := e.screen(); strings.Contains(screen, "why hello?") || !strings.Contains(screen, "Two things.") {
		t.Errorf("open filter shows the addressed thread or hides the open summary:\n%s", screen)
	}
	e.press("F")
	if screen := e.screen(); !strings.Contains(screen, "why hello?") || strings.Contains(screen, "Two things.") {
		t.Errorf("addressed filter:\n%s", screen)
	}

	// o opens the line thread in its commit.
	e.cursorTo(func(r row) bool { return r.kind == rowThread && r.thread.ID == line.ID })
	e.press("o")
	v := e.m.views[e.m.current]
	cur := e.m.main.rows[e.m.main.cursor]
	if v.kind != viewCommit || v.commit.SHA != commit || cur.kind != rowThread || cur.thread.ID != line.ID {
		t.Errorf("enter opened %q at %+v", e.m.viewLabel(v), cur)
	}
}

func TestResolvedThreadsCollapse(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
	e.press("c")
	e.typeText("needs a test")
	e.pressRun("ctrl+s")
	e.cursorTo(func(r row) bool { return r.kind == rowThread })
	thread := func() row { return e.m.main.rows[e.m.main.cursor] }
	if thread().collapsed {
		t.Fatal("open thread starts collapsed")
	}

	e.pressRun("x")
	e.cursorTo(func(r row) bool { return r.kind == rowThread })
	if !thread().collapsed || e.m.main.rowHeight(e.m.main.cursor) != 1 {
		t.Fatalf("resolved thread is not one collapsed line")
	}
	if !strings.Contains(e.screen(), "> #1 resolved") {
		t.Errorf("collapsed thread line missing:\n%s", e.screen())
	}
	e.press("enter")
	if thread().kind != rowThread || thread().collapsed {
		t.Error("enter did not expand the resolved thread")
	}
	e.press("z", "c")
	if !thread().collapsed {
		t.Error("zc did not collapse the thread")
	}

	// Reopening expands it again.
	e.pressRun("x")
	e.cursorTo(func(r row) bool { return r.kind == rowThread })
	if thread().collapsed {
		t.Error("reopened thread is still collapsed")
	}
}

// TestResolvingKeepsTheCursorStill checks that a row collapsing above or at the cursor does not
// scroll the view: the cursor row stays on the same screen line.
func TestResolvingKeepsTheCursorStill(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	for _, body := range []string{"first", "second", "third"} {
		if _, err := e.svc.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeChange, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	e.m.Update(tea.WindowSizeMsg{Width: 120, Height: 16})
	e.drain(e.m.loadCmd())
	e.openNav("Overview")
	e.press("t", "t")
	before := e.m.main.cursorY()
	id := e.m.main.rows[e.m.main.cursor].thread.ID
	e.pressRun("x")
	if got := e.m.main.rows[e.m.main.cursor]; got.kind != rowThread || got.thread.ID != id {
		t.Fatalf("cursor moved off the resolved thread to %+v", got)
	}
	if after := e.m.main.cursorY(); after != before {
		t.Errorf("cursor moved from screen line %d to %d", before, after)
	}
}

func TestContextualHelp(t *testing.T) {
	e := newTUIEnv(t)
	e.m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	bar := func() string { return ansi.Strip(e.m.renderStatus()) }

	e.openNav("Overview")
	if !strings.Contains(bar(), "c comment") || !strings.Contains(bar(), "S submit") {
		t.Errorf("overview hints = %q", bar())
	}
	e.press("h")
	if !strings.Contains(bar(), "enter open") || !strings.Contains(bar(), "S submit") {
		t.Errorf("sidebar hints = %q", bar())
	}
	e.press("l")
	e.openNav("say hello")
	e.cursorTo(addedLine)
	if !strings.Contains(bar(), "c comment") || !strings.Contains(bar(), "v select lines") {
		t.Errorf("diff line hints = %q", bar())
	}

	e.press("?")
	help := ansi.Strip(e.m.render())
	for _, want := range []string{"Diff (here)", "Overview", "Sidebar", "Folds", "zo/zc/za"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q:\n%s", want, help)
		}
	}
	if strings.Index(help, "Diff (here)") > strings.Index(help, "Sidebar") {
		t.Error("the current context is not listed first")
	}
}

func TestOverviewEnterExpandsResolved(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	v, err := e.svc.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeChange, Body: "done?"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetThreadState(ctx, v.ID, store.ThreadResolved); err != nil {
		t.Fatal(err)
	}
	e.drain(e.m.loadCmd())
	e.openNav("Overview")
	e.cursorTo(func(r row) bool { return r.kind == rowThread })
	if !e.m.main.rows[e.m.main.cursor].collapsed {
		t.Fatal("resolved thread not collapsed")
	}
	e.press("enter")
	if e.m.main.rows[e.m.main.cursor].collapsed {
		t.Error("enter did not expand the resolved thread in the overview")
	}
}

func TestOverviewGroupsReviews(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	start := func(body string) {
		if _, err := e.svc.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeChange, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	start("from the first round")
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictRequestChanges, ""); err != nil {
		t.Fatal(err)
	}
	e.repo.Write("more.go", "package main\n")
	e.repo.Commit("more")
	if _, _, err := e.svc.Sync(ctx, e.chg); err != nil {
		t.Fatal(err)
	}
	start("from the second round")
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictApprove, ""); err != nil {
		t.Fatal(err)
	}
	start("not yet submitted")
	e.m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	e.drain(e.m.loadCmd())
	e.openNav("Overview")

	screen := e.screen()
	order := []string{
		"Review #1  Human  requested changes  revision 1", "from the first round",
		"--- branch updated: revision 1 -> 2", "Review #2  Human  approved  revision 2", "from the second round",
		"Your pending review  1 new threads", "not yet submitted",
	}
	last := -1
	for _, want := range order {
		i := strings.Index(screen, want)
		if i < 0 || i < last {
			t.Fatalf("%q missing or out of order in:\n%s", want, screen)
		}
		last = i
	}
}

func TestCommentFromTheSidebar(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	// Leave the diff cursor on a line, then comment from the commit list.
	e.cursorTo(addedLine)
	e.press("h", "ctrl+k")
	if !strings.Contains(ansi.Strip(e.m.renderStatus()), "C comment on commit") {
		t.Errorf("commit list hints = %q", ansi.Strip(e.m.renderStatus()))
	}
	e.press("C")
	if e.m.mode != modeCompose || !strings.HasPrefix(e.m.compose.title, "Comment on commit") {
		t.Fatalf("C in the commit list: mode %d, title %q", e.m.mode, e.m.compose.title)
	}
	e.press("esc", "ctrl+j")
	e.press("C")
	if e.m.mode != modeCompose || !strings.Contains(e.m.compose.title, review.CommitMsgPath) {
		t.Errorf("C in the file list: mode %d, title %q, want the selected file", e.m.mode, e.m.compose.title)
	}
}

// readerEnv has an overview with one thread of three comments, one with a long body, and a second
// thread.
func readerEnv(t *testing.T, height int) (*tuiEnv, review.ThreadView) {
	t.Helper()
	e := newTUIEnv(t)
	ctx := t.Context()
	first, err := e.svc.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeChange, Body: "first comment"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeChange, Body: "second thread"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("long reply line\n\n", 30)
	for _, body := range []string{"short reply", long} {
		if _, err := e.svc.Reply(ctx, e.chg, first.ID, body, false); err != nil {
			t.Fatal(err)
		}
	}
	e.m.Update(tea.WindowSizeMsg{Width: 120, Height: height})
	e.drain(e.m.loadCmd())
	e.openNav("Overview")
	return e, first
}

func TestReaderStepsByComment(t *testing.T) {
	e, first := readerEnv(t, 30)
	p := &e.m.main
	// The first thread's three comments are three items, then the second thread.
	var rows []int
	for _, it := range p.items {
		rows = append(rows, it.row)
	}
	if len(p.items) != 4 || rows[0] != rows[1] || rows[1] != rows[2] || rows[2] == rows[3] {
		t.Fatalf("items = %+v", p.items)
	}

	e.press("g")
	sel := func() item { return p.items[p.item] }
	e.press("j")
	if p.item != 1 || p.rows[p.cursor].thread.ID != first.ID {
		t.Fatalf("j selected item %d", p.item)
	}
	// The thread's top and the selected comment are both in view.
	if top := p.starts[sel().row]; top < p.offset || sel().end > p.offset+p.height {
		t.Errorf("thread top %d or comment end %d out of view at offset %d", top, sel().end, p.offset)
	}

	// The long comment is read in half pages before moving on.
	e.press("j")
	long := sel()
	if p.item != 2 || long.end-long.start <= p.height {
		t.Fatalf("item %d of %d lines, want the long reply", p.item, long.end-long.start)
	}
	steps := 0
	for p.item == 2 && steps < 20 {
		before := p.offset
		e.press("j")
		steps++
		if p.item == 2 && p.offset <= before {
			t.Fatalf("j inside the long comment did not scroll")
		}
	}
	if steps < 2 || p.item != 3 {
		t.Errorf("left the long comment after %d steps at item %d", steps, p.item)
	}

	// k reads back up through it, then to the short reply.
	for p.item != 1 && steps < 40 {
		e.press("k")
		steps++
	}
	if p.item != 1 {
		t.Errorf("k never returned to the short reply")
	}
}

func TestReaderMarksAndEditsTheSelectedComment(t *testing.T) {
	e, first := readerEnv(t, 30)
	p := &e.m.main
	e.press("g", "j")
	// The margin bar spans the selected comment only.
	marked := 0
	marker := e.m.r.hunkMarker(rowState{activeHunk: true})
	for _, l := range p.visible(e.m.renderMainRow) {
		if strings.HasPrefix(l, marker) {
			marked++
		}
	}
	if want := p.items[p.item].end - p.items[p.item].start; marked != want {
		t.Errorf("marked %d lines, want the selected item's %d", marked, want)
	}
	// e edits the selected comment, not the latest one.
	e.press("e")
	if e.m.mode != modeCompose || e.m.compose.area.Value() != "short reply" {
		t.Errorf("e opened %q, want the selected comment", e.m.compose.area.Value())
	}
	_ = first
}

func TestReaderScrollSelectsAtReadingPosition(t *testing.T) {
	e, _ := readerEnv(t, 30)
	p := &e.m.main
	e.press("g", "d")
	line := p.offset + p.margin()
	it := p.items[p.item]
	if !(it.start <= line && line < it.end) && it.start < line {
		t.Errorf("after d, item %+v does not cover or follow the reading line %d", it, line)
	}
}

func TestSearchHighlights(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("All changes")
	e.press("/")
	e.typeText("hello")
	e.press("enter")
	match := e.m.r.st.searchMatch.Render("hello")
	if !strings.Contains(e.m.render(), match) {
		t.Errorf("match not highlighted:\n%q", e.m.render())
	}
	// The rest of the line keeps its text.
	if !strings.Contains(e.screen(), `println("hello")`) {
		t.Errorf("highlighting changed the line's text:\n%s", e.screen())
	}
	e.press("esc")
	if strings.Contains(e.m.render(), match) || strings.Contains(ansi.Strip(e.m.renderStatus()), "/hello") {
		t.Error("esc did not clear the search")
	}
}

func TestMatchSpans(t *testing.T) {
	got := matchSpans("Foo foo FOO", "foo")
	if len(got) != 3 || got[1] != [2]int{4, 7} {
		t.Errorf("smartcase spans = %v", got)
	}
	if got := matchSpans("Foo foo", "Foo"); len(got) != 1 {
		t.Errorf("case-sensitive spans = %v", got)
	}
}

// TestReaderKeepsTheThreadInView selects the last comment of a thread that fits on screen and
// checks the thread's top, where its code is, stays in view.
func TestReaderKeepsTheThreadInView(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	commit := e.m.data.commits[0].SHA
	v, err := e.svc.StartThread(ctx, e.chg, review.NewThread{
		Scope: store.ScopeCommit, Commit: commit, Path: "main.go", StartLine: 4, Body: "why?",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []string{"some", "more", "replies"} {
		if _, err := e.svc.Reply(ctx, e.chg, v.ID, b, false); err != nil {
			t.Fatal(err)
		}
	}
	for range 6 {
		if _, err := e.svc.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeChange, Body: "later\n\nthread\n\nfiller"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	e.m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	e.drain(e.m.loadCmd())
	e.openNav("Overview")
	p := &e.m.main
	// Walk to the line thread's last comment.
	for range 20 {
		if it := p.items[p.item]; p.rows[it.row].thread.ID == v.ID && p.itemSegment() == 3 {
			break
		}
		e.press("j")
	}
	it := p.items[p.item]
	if p.itemSegment() != 3 {
		t.Fatalf("never reached the last comment")
	}
	if top := p.starts[it.row]; top < p.offset {
		t.Errorf("the thread's code scrolled out: top %d above offset %d", top, p.offset)
	}
	if !strings.Contains(e.screen(), `println("hello")`) {
		t.Errorf("excerpt not on screen:\n%s", e.screen())
	}
}

func TestReviewBarColorsTheVerdict(t *testing.T) {
	e := newTUIEnv(t)
	if _, err := e.svc.Submit(t.Context(), e.chg, store.VerdictRequestChanges, "fix"); err != nil {
		t.Fatal(err)
	}
	e.drain(e.m.loadCmd())
	e.openNav("Overview")
	st := e.m.r.st
	verdict := verdictStyle(st, store.VerdictRequestChanges).Background(st.headingBg).Render("requested changes")
	if !strings.Contains(e.m.render(), verdict) {
		t.Error("the review bar's verdict is not in its own color")
	}
}

func TestReviewedEntriesAndHeadersAreStyled(t *testing.T) {
	st := newStyles(true, ThemeDefault)
	done := navItem{label: "M a.go", badge: "(2) *x", done: true}
	got := done.render(st, 30, false, false)
	if !strings.Contains(got, st.muted.Render(fit("M a.go", 23, nil))) {
		t.Errorf("reviewed entry not dimmed: %q", got)
	}
	for _, part := range []string{st.badgeOpen.Bold(true).Render(markReviewed), st.badgeWarn.Bold(true).Render(markDraft)} {
		if !strings.Contains(got, part) {
			t.Errorf("badge part %q not colored in %q", part, got)
		}
	}

	e := newTUIEnv(t)
	e.openNav("All changes")
	e.cursorTo(func(r row) bool { return r.kind == rowFile })
	e.pressRun("m")
	e.cursorTo(func(r row) bool { return r.kind == rowFile })
	header := ansi.Strip(e.m.renderMainRow(e.m.main.cursor, rowState{})[0])
	if !strings.Contains(header, "reviewed  collapsed, enter expands") {
		t.Errorf("file header = %q", header)
	}
	key := st.helpKey.Background(st.headingBg).Render("enter")
	if !strings.Contains(e.m.renderMainRow(e.m.main.cursor, rowState{})[0], key) {
		t.Error("enter not styled as a key in the file header")
	}
}

func TestEditingAResolvedThreadWarns(t *testing.T) {
	e := newTUIEnv(t)
	ctx := t.Context()
	v, err := e.svc.StartThread(ctx, e.chg, review.NewThread{Scope: store.ScopeChange, Body: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Submit(ctx, e.chg, store.VerdictComment, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetThreadState(ctx, v.ID, store.ThreadResolved); err != nil {
		t.Fatal(err)
	}
	e.drain(e.m.loadCmd())
	e.openNav("Overview")
	e.cursorTo(func(r row) bool { return r.kind == rowThread })
	e.press("e")
	if e.m.mode != modeCompose || !e.m.compose.warn || !strings.Contains(e.m.compose.title, "will not reopen") {
		t.Errorf("editing a resolved thread: mode %d, title %q", e.m.mode, e.m.compose.title)
	}
}

func TestColorblindTheme(t *testing.T) {
	for _, dark := range []bool{true, false} {
		def, cb := newStyles(dark, ThemeDefault), newStyles(dark, ThemeColorblind)
		if cb.addedBg == def.addedBg || cb.deletedBg == def.deletedBg {
			t.Errorf("dark %v: colorblind theme keeps the default diff backgrounds", dark)
		}
		// No green or red: approvals and additions share the accent's blue.
		if cb.badgeOpen.GetForeground() != cb.accent {
			t.Errorf("dark %v: colorblind good color is not blue", dark)
		}
	}
}
