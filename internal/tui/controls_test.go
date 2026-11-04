package tui

import (
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/look"
)

// onScreen is a control on the screen: its zone id, where it is, what it runs, and whether it runs
// in the mode open now.
type onScreen struct {
	id    string
	zone  look.Zone
	title string
	live  bool
}

// onScreen are the controls on the screen drawn at where, in reading order: "box", "row" for every
// row's, or "row12:" for one row's.
func (e *tuiEnv) onScreen(where string) []onScreen {
	e.m.render()
	var out []onScreen
	for id, zones := range e.m.zones {
		c, ok := e.m.drawnControls[id]
		if !ok || !strings.HasPrefix(id, controlZone+where) {
			continue
		}
		out = append(out, onScreen{id: id, zone: zones[0], title: c.run.Title, live: c.mode == e.m.mode})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].zone, out[j].zone
		return a.Y < b.Y || a.Y == b.Y && a.X < b.X
	})
	return out
}

// shownControls are the titles of the controls on the screen drawn at where that a click runs now,
// in reading order.
func (e *tuiEnv) shownControls(where string) []string {
	var out []string
	for _, c := range e.onScreen(where) {
		if c.live {
			out = append(out, c.title)
		}
	}
	return out
}

// click clicks, pressing and letting go, the first control on the screen drawn at where whose action is titled title, whether
// or not it runs now.
func (e *tuiEnv) click(where, title string) {
	e.t.Helper()
	for _, c := range e.onScreen(where) {
		if c.title == title {
			_, cmd := e.m.Update(tea.MouseClickMsg{X: c.zone.X, Y: c.zone.Y, Button: tea.MouseLeft})
			e.drain(cmd)
			_, cmd = e.m.Update(tea.MouseReleaseMsg{X: c.zone.X, Y: c.zone.Y, Button: tea.MouseLeft})
			e.drain(cmd)
			return
		}
	}
	e.t.Fatalf("no control %q at %q among %q:\n%s", title, where, e.shownControls(where), e.screen())
}

// hoverRow moves the mouse over row i of the main pane.
func (e *tuiEnv) hoverRow(i int) {
	e.t.Helper()
	nw, _, _ := e.m.layoutSizes()
	_, cmd := e.m.Update(tea.MouseMotionMsg{X: nw + 10, Y: e.m.main.starts[i] - e.m.main.offset + e.m.mainTop()})
	e.drain(cmd)
}

// The comment box and the prompts show their actions as controls, which a click runs as their keys
// do.
func TestControls(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
	bodies := func() []string {
		t.Helper()
		threads, err := e.svc.Threads(t.Context(), e.chg, review.ThreadFilter{})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, th := range threads {
			out = append(out, th.Comments[0].Body)
		}
		return out
	}

	e.press("c")
	if got, want := e.shownControls(""), []string{"save", "close, asking if unsaved", "open $EDITOR"}; !reflect.DeepEqual(got, want) {
		t.Errorf("comment box controls = %q, want %q", got, want)
	}
	e.typeText("first")
	// The rows behind the box keep their controls' look, and do nothing.
	e.click("row", "mark reviewed")
	if e.m.mode != modeCompose || e.m.compose.area.Value() != "first" || e.m.fileReviewed(e.m.views[e.m.current], 1) {
		t.Fatalf("a row's control behind the box left mode %d, text %q, reviewed %v", e.m.mode, e.m.compose.area.Value(),
			e.m.fileReviewed(e.m.views[e.m.current], 1))
	}
	e.click("box", "close, asking if unsaved")
	if got, want := e.shownControls(""), []string{"save", "close, asking if unsaved", "open $EDITOR", "save", "discard it", "keep editing"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unsaved comment controls = %q, want %q", got, want)
	}
	e.click("closing", "keep editing")
	if e.m.mode != modeCompose || e.m.compose.closing {
		t.Fatalf("keep editing left mode %d, closing %v", e.m.mode, e.m.compose.closing)
	}
	e.press("esc")
	e.click("closing", "discard it")
	if e.m.mode != modeNormal || len(bodies()) != 0 {
		t.Fatalf("discarding left mode %d, threads %q", e.m.mode, bodies())
	}

	e.press("c")
	e.typeText("second")
	e.click("box", "save")
	if got, want := bodies(), []string{"second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("saving gave threads %q, want %q", got, want)
	}

	e.press("S")
	if got, want := e.shownControls(""), []string{"approve", "request changes", "comment", "cancel"}; !reflect.DeepEqual(got, want) {
		t.Errorf("verdict controls = %q, want %q", got, want)
	}
	e.click("verdict", "cancel")
	if e.m.mode != modeNormal {
		t.Fatalf("cancel left mode %d", e.m.mode)
	}

	e.press("D")
	if got, want := e.shownControls(""), []string{"yes", "no"}; !reflect.DeepEqual(got, want) {
		t.Errorf("confirm controls = %q, want %q", got, want)
	}
	e.click("confirm", "no")
	if e.m.mode != modeNormal || len(bodies()) != 1 {
		t.Fatalf("no left mode %d, threads %q", e.m.mode, bodies())
	}
	e.press("D")
	e.click("confirm", "yes")
	if e.m.mode != modeNormal || len(bodies()) != 0 {
		t.Fatalf("yes left mode %d, threads %q", e.m.mode, bodies())
	}
	if !strings.Contains(e.m.status, "discarded") {
		t.Errorf("status %q, want the draft discarded", e.m.status)
	}
}

// A file header's Viewed box, a fold's Expand, the + on the cursor's line, and the hints run what
// their keys do.
func TestRowControls(t *testing.T) {
	e := newTUIEnv(t)
	lines := gittest.Numbered(30)
	e.repo.Write("long.txt", gittest.Lines(lines...))
	e.repo.Commit("add long file")
	lines[1], lines[27] = "changed 2", "changed 28"
	e.repo.Write("long.txt", gittest.Lines(lines...))
	e.repo.Commit("edit both ends")
	e.drain(e.m.loadCmd())
	e.openNav("edit both ends")
	e.pressRun("l")
	gaps := func() int {
		n := 0
		for _, r := range e.m.main.rows {
			if r.kind == rowGap {
				n++
			}
		}
		return n
	}

	before := gaps()
	e.click("row", "expand")
	if got := gaps(); got != before-1 {
		t.Errorf("expanding a fold left %d of %d", got, before)
	}

	e.cursorTo(func(r row) bool { return addedLine(r) && r.file == 1 })
	if got, want := e.shownControls("row"), []string{"fold", "mark reviewed", "expand"}; !reflect.DeepEqual(got, want) {
		t.Errorf("row controls with the cursor on a line = %q, want %q: the + shows on hover", got, want)
	}
	e.hoverRow(e.m.main.cursor - 1)
	e.hoverRow(e.m.main.cursor)
	if got, want := e.shownControls("row"), []string{"fold", "mark reviewed", "comment", "expand"}; !reflect.DeepEqual(got, want) {
		t.Errorf("row controls with the mouse over a line = %q, want %q", got, want)
	}
	e.click("row", "comment")
	if e.m.mode != modeCompose || e.m.compose.title != "Comment on long.txt:2 in commit "+shortSHA(e.m.views[e.m.current].commit.SHA) {
		t.Fatalf("the + opened mode %d, %q", e.m.mode, e.m.compose.title)
	}
	e.press("esc")

	e.click("row", "mark reviewed")
	if r := e.m.main.rows[e.m.main.cursor]; r.kind != rowFile || !r.reviewed {
		t.Errorf("after the Viewed box, the cursor is on %+v, want the reviewed file", r)
	}

	e.click("hints", "help")
	if e.m.mode != modeHelp {
		t.Errorf("the help hint left mode %d", e.m.mode)
	}
}

// The + takes the margin of the message's line under the mouse too.
func TestCommentOnTheMessageFromItsMargin(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.pressRun("l")
	e.cursorTo(func(r row) bool { return r.kind == rowLine && isMessage(e.m.views[e.m.current].files[r.file]) })
	e.hoverRow(e.m.main.cursor)
	e.click("row", "comment")
	if want := "Comment on " + review.CommitMsgPath + ":1 in commit " + shortSHA(e.m.views[e.m.current].commit.SHA); e.m.mode != modeCompose || e.m.compose.title != want {
		t.Errorf("the + opened mode %d, %q, want %q", e.m.mode, e.m.compose.title, want)
	}
}

// An open thread folds from the chevron heading its box, and unfolds from the one heading its line.
func TestOpenThreadFoldsFromItsChevron(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("Overview")
	e.pressRun("l")
	onThread := func(r row) bool { return r.kind == rowThread && r.thread.Seq == 1 }
	e.cursorTo(onThread)
	at := "row" + strconv.Itoa(e.m.main.cursor) + ":"
	e.click(at, "expand")
	e.cursorTo(onThread)
	if r := e.m.main.rows[e.m.main.cursor]; !r.collapsed {
		t.Fatalf("after its chevron, thread #1 is open:\n%s", e.screen())
	}
	e.click("row"+strconv.Itoa(e.m.main.cursor)+":", "expand")
	e.cursorTo(onThread)
	if r := e.m.main.rows[e.m.main.cursor]; r.collapsed {
		t.Errorf("after its chevron again, thread #1 is folded:\n%s", e.screen())
	}
}

// A thread's buttons, a review's Delete, a commit's Viewed box, Prev and Next, and the overview's
// filter run what their keys do.
func TestThreadAndTitleControls(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("Overview")
	e.pressRun("l")
	e.cursorTo(func(r row) bool { return r.kind == rowThread && r.thread.Seq == 2 })
	at := "row" + strconv.Itoa(e.m.main.cursor) + ":"
	if got, want := e.shownControls(at), []string{"expand", "reply", "resolve", "edit", "delete"}; !reflect.DeepEqual(got, want) {
		t.Errorf("an addressed thread's controls = %q, want %q", got, want)
	}
	e.click(at, "resolve")
	if r := e.m.main.rows[e.m.main.cursor]; r.kind != rowThread || r.thread.Seq != 2 || r.thread.State != store.ThreadResolved {
		t.Errorf("after Resolve, the cursor is on %+v, want thread #2 resolved", r)
	}

	e.cursorTo(func(r row) bool { return r.kind == rowReview })
	e.click("row"+strconv.Itoa(e.m.main.cursor)+":", "delete")
	if e.m.mode != modeConfirm {
		t.Errorf("a review's Delete left mode %d, want it to ask", e.m.mode)
	}
	e.press("n")

	e.click("title", "filter")
	if e.m.filter != filterUnresolved {
		t.Errorf("the filter shows %s, want unresolved", e.m.filter)
	}

	e.press("h")
	e.openNav("say hello")
	e.pressRun("l")
	if got, want := e.shownControls("title"), []string{"next commit", "next file"}; !reflect.DeepEqual(got, want) {
		t.Errorf("the first commit's title controls = %q, want %q: Prev and Prev file greyed", got, want)
	}
	e.click("row", "mark reviewed")
	if !e.m.viewReviewed(e.m.views[e.m.current]) {
		t.Error("the commit's Viewed box did not mark it reviewed")
	}
	e.click("title", "next commit")
	if got, want := e.shownControls("title"), []string{"previous commit", "next file"}; !reflect.DeepEqual(got, want) || e.m.views[e.m.current].commit.Subject() != "Add a greeting helper" {
		t.Errorf("after Next, commit %q has title controls %q, want the last with %q", e.m.views[e.m.current].commit.Subject(), got, want)
	}
}

// Dragging from the + selects the lines it passes, and letting go comments on them, as on GitHub.
// Moving off the lines leaves the selection where it was.
func TestDragTheCommentPlus(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("Add a greeting helper")
	e.pressRun("l")
	e.cursorTo(func(r row) bool { return addedLine(r) && r.line.NewLine == 2 })
	start := e.m.main.cursor
	e.hoverRow(start)
	var plus look.Zone
	for _, c := range e.onScreen("row" + strconv.Itoa(start) + ":") {
		if c.title == "comment" {
			plus = c.zone
		}
	}
	if plus.W == 0 {
		t.Fatalf("no + on the line under the mouse:\n%s", e.screen())
	}
	y := func(i int) int { return e.m.main.starts[i] - e.m.main.offset + e.m.mainTop() }
	e.m.Update(tea.MouseClickMsg{X: plus.X, Y: plus.Y, Button: tea.MouseLeft})
	e.m.Update(tea.MouseMotionMsg{X: plus.X, Y: y(start + 2), Button: tea.MouseLeft})
	// Below the last line, the selection stays where it was.
	e.m.Update(tea.MouseMotionMsg{X: plus.X, Y: e.m.main.height, Button: tea.MouseLeft})
	_, cmd := e.m.Update(tea.MouseReleaseMsg{X: plus.X, Y: e.m.main.height, Button: tea.MouseLeft})
	e.drain(cmd)
	want := "Comment on util.go:2-4 in commit " + shortSHA(e.m.views[e.m.current].commit.SHA)
	if e.m.mode != modeCompose || e.m.compose.title != want {
		t.Errorf("dragging the + opened mode %d, %q, want %q", e.m.mode, e.m.compose.title, want)
	}
}

// Lighting quiet controls, as the mouse reaches them, shows their keys in room kept for them, so
// nothing moves under the mouse.
func TestLitControlsStayPut(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("Overview")
	at := ""
	for i, r := range e.m.main.rows {
		if r.kind == rowThread && r.thread.Seq == 1 {
			at = "row" + strconv.Itoa(i) + ":"
		}
	}
	zones := func() map[string]look.Zone {
		out := map[string]look.Zone{}
		for _, c := range e.onScreen(at) {
			out[c.title] = c.zone
		}
		return out
	}
	quiet := zones()
	for i, r := range e.m.main.rows {
		if r.kind == rowThread && r.thread.Seq == 1 {
			e.hoverRow(i)
		}
	}
	lit := zones()
	if len(quiet) == 0 || !reflect.DeepEqual(lit, quiet) {
		t.Errorf("thread #1's controls lit at %+v, quiet at %+v; want them in the same place", lit, quiet)
	}
}

// The title bar's Next file and Prev file go where ] and [ do, and are greyed where there is no
// file that way.
func TestFileSteps(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("All changes")
	e.pressRun("l")
	if got, want := e.shownControls("title"), []string{"next file"}; !reflect.DeepEqual(got, want) {
		t.Errorf("on the first file, title controls = %q, want %q", got, want)
	}
	e.click("title", "next file")
	if r := e.m.main.rows[e.m.main.cursor]; r.kind != rowFile || r.file != 1 {
		t.Errorf("Next file went to %+v, want the second file's header", r)
	}
	if got, want := e.shownControls("title"), []string{"previous file"}; !reflect.DeepEqual(got, want) {
		t.Errorf("on the last file, title controls = %q, want %q", got, want)
	}
}

// A folded thread, as a resolved one in the overview, opens from its chevron.
func TestThreadChevronOpensIt(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("Overview")
	at := -1
	for i, r := range e.m.main.rows {
		if r.kind == rowThread && r.thread.State == store.ThreadResolved && r.collapsed {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("no folded resolved thread in the overview")
	}
	id := e.m.main.rows[at].thread.ID
	e.click("row"+strconv.Itoa(at)+":", actToggleThread.Title)
	for _, r := range e.m.main.rows {
		if r.kind == rowThread && r.thread.ID == id && r.collapsed {
			t.Errorf("clicking the chevron left thread %s folded", r.thread.Label())
		}
	}
}
