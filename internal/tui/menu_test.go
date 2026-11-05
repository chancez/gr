package tui

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/interact"
	"github.com/chancez/gr/internal/tui/look"
)

// A right click on a diff line opens its menu: the line's actions, then its file's and its
// commit's, each under a heading, and picking one runs it.
func TestRightClickMenu(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.press("l")
	y := -1
	for i, line := range strings.Split(e.screen(), "\n") {
		if strings.Contains(line, `+     println("hello")`) {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("no added line on screen:\n%s", e.screen())
	}
	nw, _, _ := e.m.layoutSizes()
	cursor := e.m.main.cursor
	_, cmd := e.m.Update(tea.MouseClickMsg{X: nw + 10, Y: y, Button: tea.MouseRight})
	e.drain(cmd)
	if e.m.mode != modePicker {
		t.Fatalf("right click opened no menu: mode %d", e.m.mode)
	}
	if e.m.main.cursor != cursor {
		t.Errorf("right click moved the cursor from %d to %d", cursor, e.m.main.cursor)
	}
	// The menu's top left corner is just right of the click, and it grows down, shifted up only as
	// far as it needs to fit.
	e.m.render()
	p := e.m.picker
	_, _, bh := e.m.layoutSizes()
	if p.x != nw+11 || p.y != min(y, bh-p.height) {
		t.Errorf("menu at %d,%d with height %d, clicked at %d,%d", p.x, p.y, p.height, nw+10, y)
	}
	if got := shownItems(e.m.picker.items); !reflect.DeepEqual(got, lineMenu) {
		t.Errorf("menu = %+v, want %+v", got, lineMenu)
	}
	// A second right click, with the menu open, opens the menu of where it lands.
	_, cmd = e.m.Update(tea.MouseClickMsg{X: nw + 30, Y: y, Button: tea.MouseRight})
	e.drain(cmd)
	e.m.render()
	if e.m.mode != modePicker || e.m.picker.x != nw+31 {
		t.Fatalf("a second right click left mode %d, menu at x %d, want a new menu at %d", e.m.mode, e.m.picker.x, nw+31)
	}
	// The file's comment, under the line's.
	e.press("down", "down", "down")
	e.pressRun("enter")
	if e.m.mode != modeCompose || !strings.Contains(e.m.compose.title, "main.go") {
		t.Errorf("picking comment on file opened mode %d, title %q", e.m.mode, e.m.compose.title)
	}
}

// ctrl+p lists the cursor's actions flat, each naming its subject.
func TestPalette(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.press("l")
	e.cursorTo(func(r row) bool { return addedLine(r) && strings.Contains(r.line.Text, "println") })
	e.pressRun("ctrl+p")
	if e.m.mode != modePicker {
		t.Fatalf("ctrl+p opened no palette: mode %d", e.m.mode)
	}
	found := false
	for _, it := range e.m.picker.items {
		if it.value == "File main.go: mark reviewed" {
			found = true
		}
	}
	if !found {
		t.Errorf("the palette has no file action named with its subject: %+v", e.m.picker.items)
	}
}

// A menu's top left corner is just right of its point, and near the edges it shifts only enough to
// fit.
func TestAnchoredLayout(t *testing.T) {
	items := make([]pickerItem, 10)
	for i := range items {
		items[i] = pickerItem{value: "action"}
	}
	for _, tc := range []struct {
		x, y   int
		wantX  int
		wantY  int
		reason string
	}{
		{x: 10, y: 5, wantX: 11, wantY: 5, reason: "room below"},
		{x: 10, y: 35, wantX: 11, wantY: 40 - 15, reason: "at the bottom"},
		{x: 110, y: 5, wantX: 120 - 40, wantY: 5, reason: "at the right edge"},
	} {
		p := newPicker("Actions", items, nil)
		p.anchored, p.anchorX, p.anchorY = true, tc.x, tc.y
		p.layout(120, 40)
		if p.x != tc.wantX || p.y != tc.wantY {
			t.Errorf("%s: menu at %d,%d, want %d,%d", tc.reason, p.x, p.y, tc.wantX, tc.wantY)
		}
	}
}

// A sha in a thread's location is a mention: a click on it opens the commit's menu, and opening the
// commit from it shows that commit.
func TestClickingAMention(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("Overview")
	e.m.render()
	var zone []look.Zone
	for id, zs := range e.m.zones {
		if _, ok := commitMentions.At(e.m, id); ok {
			zone = zs
		}
	}
	if len(zone) == 0 {
		t.Fatalf("no commit mention on the overview:\n%s", e.screen())
	}
	_, cmd := e.m.Update(tea.MouseClickMsg{X: zone[0].X, Y: zone[0].Y, Button: tea.MouseLeft})
	e.drain(cmd)
	if e.m.mode != modePicker || !strings.HasPrefix(e.m.picker.title, "Commit ") {
		t.Fatalf("clicking the sha opened mode %d, %+v", e.m.mode, e.m.picker)
	}
	e.typeText("open")
	e.pressRun("enter")
	if v := e.m.views[e.m.current]; v.kind != viewCommit {
		t.Errorf("opening the mentioned commit shows view kind %d", v.kind)
	}
}

// A right click on a sidebar entry opens its menu without selecting it; only "open" moves there.
func TestRightClickInTheSidebarLeavesTheSelection(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("Overview")
	pos, current := e.m.viewsNav.pos, e.m.current
	y := -1
	for i, line := range strings.Split(e.screen(), "\n") {
		if strings.Contains(line, "say hello") {
			y = i
			break
		}
	}
	_, cmd := e.m.Update(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseRight})
	e.drain(cmd)
	if e.m.mode != modePicker || e.m.viewsNav.pos != pos || e.m.current != current {
		t.Fatalf("right click: mode %d, selection %d (was %d), view %d (was %d)", e.m.mode, e.m.viewsNav.pos, pos, e.m.current, current)
	}
	e.typeText("open")
	e.pressRun("enter")
	if v := e.m.views[e.m.current]; v.kind != viewCommit || v.commit.Subject() != "say hello" {
		t.Errorf("open showed %q", v.commit.Subject())
	}
}

// Editing and deleting are offered only on threads with a comment of yours.
func TestMenuOffersOnlyYourComments(t *testing.T) {
	e := newTUIEnv(t)
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
	e.drain(e.m.loadCmd())
	e.openNav("Overview")
	e.cursorTo(func(r row) bool { return r.kind == rowThread })
	t1, _ := e.m.cursorTarget()
	// A thread on the whole change has no code to open.
	if got, want := menuTitles(e.m, t1), []string{"expand", "reply", "resolve", "addressed", "filter", "comment", "submit"}; !slices.Equal(got, want) {
		t.Errorf("menu on another's thread = %q, want %q", got, want)
	}
	if bar := ansi.Strip(e.m.renderStatus()); strings.Contains(bar, "edit") || strings.Contains(bar, "delete") {
		t.Errorf("hints offer editing another's thread: %q", bar)
	}
}

// menuTitles are the titles of a target's menu, headings left out.
func menuTitles(m *model, t interact.Target[*model]) []string {
	var out []string
	for _, it := range m.actionItems(t, false) {
		if !it.heading {
			out = append(out, it.value)
		}
	}
	return out
}

// lineMenu is the menu of a line of the say hello commit's diff, away from the cursor.
var lineMenu = []pickerItem{
	{value: "Line", heading: true},
	{value: "comment", full: "Line: comment", note: "c"},
	{value: "open in $EDITOR", full: "Line: open in $EDITOR", note: "E"},
	{value: "File main.go", heading: true},
	{value: "mark reviewed", full: "File main.go: mark reviewed", note: "m"},
	{value: "comment", full: "File main.go: comment", note: "C"},
	{value: "open in $EDITOR", full: "File main.go: open in $EDITOR"},
	{value: "Commit b194130", heading: true},
	{value: "mark reviewed", full: "Commit b194130: mark reviewed", note: "M"},
	{value: "comment", full: "Commit b194130: comment"},
}

// shownItems are picker items as they show, without what picking one runs, to compare.
func shownItems(items []pickerItem) []pickerItem {
	out := make([]pickerItem, len(items))
	for i, it := range items {
		it.run = nil
		out[i] = it
	}
	return out
}

// Marking a mentioned commit reviewed stays where it was, even with auto-advance.
func TestMarkingAMentionStaysPut(t *testing.T) {
	e := goldenEnv(t)
	e.m.autoAdvance = true
	e.openNav("Overview")
	e.m.render()
	var zone []look.Zone
	for id, zs := range e.m.zones {
		if _, ok := commitMentions.At(e.m, id); ok {
			zone = zs
		}
	}
	if len(zone) == 0 {
		t.Fatalf("no commit mention on the overview:\n%s", e.screen())
	}
	current := e.m.current
	_, cmd := e.m.Update(tea.MouseClickMsg{X: zone[0].X, Y: zone[0].Y, Button: tea.MouseRight})
	e.drain(cmd)
	e.typeText("mark reviewed")
	e.pressRun("enter")
	if e.m.current != current {
		t.Errorf("marking a mentioned commit moved from view %d to %d", current, e.m.current)
	}
	if !strings.Contains(e.m.status, "reviewed") {
		t.Errorf("status %q, want the commit marked", e.m.status)
	}
}

// A search matching part of a mentioned sha leaves the whole sha clickable.
func TestSearchKeepsMentions(t *testing.T) {
	e := goldenEnv(t)
	e.openNav("Overview")
	e.m.render()
	var want map[string][]look.Zone
	sha := ""
	for id, zs := range e.m.zones {
		if t, ok := commitMentions.At(e.m, id); ok {
			want = map[string][]look.Zone{id: zs}
			sha = t.Groups(e.m)[0].Label
		}
	}
	if want == nil {
		t.Fatalf("no commit mention on the overview:\n%s", e.screen())
	}
	e.press("/")
	e.typeText(strings.TrimPrefix(sha, "Commit ")[:4])
	e.press("enter")
	e.m.render()
	got := map[string][]look.Zone{}
	for id, zs := range e.m.zones {
		if _, ok := want[id]; ok {
			got[id] = zs
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("zones after searching %q = %+v, want %+v", sha, got, want)
	}
}

// New data closes a menu, whose actions are bound to rows that are built again.
func TestNewDataClosesAMenu(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.press("l")
	nw, _, _ := e.m.layoutSizes()
	y := strings.Index(e.screen(), "println")
	y = strings.Count(e.screen()[:y], "\n")
	_, cmd := e.m.Update(tea.MouseClickMsg{X: nw + 10, Y: y, Button: tea.MouseRight})
	e.drain(cmd)
	if e.m.mode != modePicker {
		t.Fatalf("right click opened no menu: mode %d", e.m.mode)
	}
	other, err := review.Open(t.Context(), e.store, e.svc.Git(), store.Author{Name: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.StartThread(t.Context(), e.chg, review.NewThread{Scope: store.ScopeChange, Body: "from the agent"}); err != nil {
		t.Fatal(err)
	}
	e.drain(e.m.loadCmd())
	if e.m.mode != modeNormal {
		t.Errorf("menu still open after new data: mode %d", e.m.mode)
	}
}

// A menu shows the keys of the pane it was opened in, not of where focus is.
func TestMenuKeysFollowTheClick(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.press("l")
	e.m.setFocus(focusFiles)
	nw, _, _ := e.m.layoutSizes()
	y := strings.Count(e.screen()[:strings.Index(e.screen(), "+     println")], "\n")
	_, cmd := e.m.Update(tea.MouseClickMsg{X: nw + 10, Y: y, Button: tea.MouseRight})
	e.drain(cmd)
	if e.m.mode != modePicker {
		t.Fatalf("right click opened no menu: mode %d", e.m.mode)
	}
	if got := shownItems(e.m.picker.items); !reflect.DeepEqual(got, lineMenu) {
		t.Errorf("menu = %+v, want %+v", got, lineMenu)
	}
}

// Moving up a page or more lands on the first entry under a leading heading, and shows the heading.
func TestPickerMovesPastAHeading(t *testing.T) {
	items := []pickerItem{{value: "Heading", heading: true}, {value: "a"}, {value: "b"}, {value: "c"}}
	for _, tc := range []struct {
		from, n int
	}{
		{from: 2, n: -3},
		{from: 3, n: -10},
	} {
		p := newPicker("Actions", items, nil)
		p.layout(120, 40)
		p.pos, p.top = tc.from, tc.from
		p.move(tc.n)
		if p.pos != 1 || p.top != 0 {
			t.Errorf("move(%d) from %d: pos %d top %d, want pos 1 top 0", tc.n, tc.from, p.pos, p.top)
		}
	}
}

// Typing in a menu hides its headings, so each entry shows what it acts on.
func TestFilteringAMenuNamesSubjects(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.press("l")
	nw, _, _ := e.m.layoutSizes()
	y := strings.Count(e.screen()[:strings.Index(e.screen(), "+     println")], "\n")
	_, cmd := e.m.Update(tea.MouseClickMsg{X: nw + 10, Y: y, Button: tea.MouseRight})
	e.drain(cmd)
	e.typeText("comment")
	e.m.render()
	var got []string
	for _, line := range e.m.picker.render(e.m.r.th)[3:6] {
		got = append(got, strings.Trim(ansi.Strip(line), " |"))
	}
	if want := []string{"Line: comment  c", "File main.go: comment  C", "Commit b194130: comment"}; !slices.Equal(got, want) {
		t.Errorf("filtered menu shows %q, want %q", got, want)
	}
}

// A right click on a divider or an indicator of more entries moves and drags nothing.
func TestRightClickOnDividersAndIndicators(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.m.viewsHeight = 3
	e.m.relayout(false)
	_, _, bh := e.m.layoutSizes()
	vh, _ := e.m.sidebarHeights(bh)
	e.m.Update(tea.MouseClickMsg{X: 2, Y: vh, Button: tea.MouseRight})
	if e.m.draggingSplit {
		t.Error("a right click on the split started a drag")
	}
	y := -1
	for i, line := range strings.Split(e.screen(), "\n") {
		if i < vh && strings.Contains(line, "more") {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("no indicator in the commit list:\n%s", e.screen())
	}
	focus, pos, current := e.m.focus, e.m.viewsNav.pos, e.m.current
	e.m.Update(tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseRight})
	if e.m.focus != focus || e.m.viewsNav.pos != pos || e.m.current != current || e.m.mode != modeNormal {
		t.Errorf("right click on an indicator: focus %d (was %d), selection %d (was %d), view %d (was %d), mode %d",
			e.m.focus, focus, e.m.viewsNav.pos, pos, e.m.current, current, e.m.mode)
	}
}

// In split mode a right click comments on the side it lands on, wherever the cursor's side is.
func TestRightClickInSplitTakesItsSide(t *testing.T) {
	e := newTUIEnv(t)
	e.openNav("say hello")
	e.press("s", "l")
	lines := strings.Split(e.screen(), "\n")
	y := -1
	for i, line := range lines {
		if strings.Contains(line, `println("hi")`) {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("no paired line:\n%s", e.screen())
	}
	x := strings.Index(lines[y], `println("hi")`)
	_, cmd := e.m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseRight})
	e.drain(cmd)
	e.typeText("Line: comment")
	e.pressRun("enter")
	if want := "Comment on main.go:4 (old) in commit b194130"; e.m.mode != modeCompose || e.m.compose.title != want {
		t.Errorf("comment from the old side: mode %d, title %q, want %q", e.m.mode, e.m.compose.title, want)
	}
}
