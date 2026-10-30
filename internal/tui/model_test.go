package tui

import (
	"path/filepath"
	"strings"
	"testing"

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
	for _, want := range []string{"#1 feature", "Overview", "All changes", "say h", "Conversation"} {
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
	got := ansi.Strip(l.render(newStyles(true), "T", 12, 2, false)[1])
	if got != " a very l..." {
		t.Errorf("truncated label = %q", got)
	}
}

func TestFileLabelsKeepTheirEnd(t *testing.T) {
	l := navList{items: []navItem{{prefix: "M ", label: "internal/anchor/anchor_test.go"}}}
	got := ansi.Strip(l.render(newStyles(true), "T", 20, 2, false)[1])
	if got != " M ...anchor_test.go" {
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
			reviewedItem = strings.HasPrefix(it.label, markReviewed)
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
	if !strings.Contains(e.m.status, "match 1 of 2") {
		t.Errorf("status = %q", e.m.status)
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
		if strings.Contains(it.label, "say hello") && !strings.HasPrefix(it.label, marks(false, true)) {
			t.Errorf("commit label %q lacks the draft mark", it.label)
		}
	}
	for _, it := range e.m.filesNav.items {
		if strings.Contains(it.label, "main.go") && !strings.HasPrefix(it.prefix, marks(false, true)) {
			t.Errorf("file prefix %q lacks the draft mark", it.prefix)
		}
		if strings.Contains(it.label, "COMMIT_MSG") && !strings.HasPrefix(it.prefix, marks(false, false)) {
			t.Errorf("message prefix %q has marks", it.prefix)
		}
	}
	if marks(true, true) != "x* " || marks(false, false) != "   " {
		t.Errorf("marks = %q and %q", marks(true, true), marks(false, false))
	}
}
