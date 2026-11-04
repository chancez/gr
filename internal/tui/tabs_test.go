package tui

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// What a tab's command says comes back marked as the tab's, a batch's commands each, and Bubble
// Tea's own messages are left for it.
func TestWrap(t *testing.T) {
	ours := func() tea.Msg { return opMsg{status: "x"} }
	if got := wrap(3, ours)(); !reflect.DeepEqual(got, tabMsg{id: 3, msg: opMsg{status: "x"}}) {
		t.Errorf("wrapped = %#v", got)
	}
	batch, ok := wrap(3, tea.Batch(ours, ours))().(tea.BatchMsg)
	if !ok || len(batch) != 2 || !reflect.DeepEqual(batch[0](), tabMsg{id: 3, msg: opMsg{status: "x"}}) {
		t.Errorf("a batch's commands are not each wrapped: %#v", batch)
	}
	if got := wrap(3, tea.Quit)(); !reflect.DeepEqual(got, tea.QuitMsg{}) {
		t.Errorf("quit wrapped as %#v, want it left for Bubble Tea", got)
	}
}

// tabsEnv is gr with tabs: the app, over a change with something of everything.
type tabsEnv struct {
	*tuiEnv
	a *app
}

func newTabsEnv(t *testing.T) *tabsEnv {
	e := goldenEnv(t)
	a := newApp(t.Context(), Options{Glyphs: "ascii"}, e.m)
	a.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return &tabsEnv{tuiEnv: e, a: a}
}

// press sends a key to the app, and hands the app what the tab hands it back.
func (e *tabsEnv) press(k string) {
	e.t.Helper()
	_, cmd := e.a.Update(keyMsg(k))
	for cmd != nil {
		msg, ok := cmd().(tabMsg)
		if !ok {
			return
		}
		switch msg.msg.(type) {
		case newTabMsg, closeTabMsg, stepTabMsg, goTabMsg:
			_, cmd = e.a.Update(msg)
		default:
			return
		}
	}
}

// bar is the tab bar's text.
func (e *tabsEnv) bar() string {
	return strings.TrimRight(ansi.Strip(e.a.tabBar()), " ")
}

func TestTabs(t *testing.T) {
	e := newTabsEnv(t)
	e.repo.Git("branch", "other")
	target, err := e.svc.ParseTarget(t.Context(), "other", "main")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := e.svc.EnsureChange(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	e.a.Update(tabMsg{id: e.a.ids[0], msg: newTabMsg{svc: e.svc, change: other}})
	tab := e.a.tabs[e.a.active]
	tab.syncLoads, tab.glyphsSet = true, true
	(&tuiEnv{t: t, m: tab}).drain(tab.loadCmd())
	if got, want := e.bar(), " #1 feature   #2 other"; e.a.active != 1 || got != want {
		t.Errorf("after opening a tab: tab %d shown, bar %q, want the new one and %q", e.a.active, got, want)
	}

	e.press("ctrl+pgup")
	if e.a.active != 0 {
		t.Errorf("ctrl+pgup showed tab %d, want the first", e.a.active)
	}
	e.press("alt+2")
	if e.a.active != 1 {
		t.Errorf("alt+2 showed tab %d, want the second", e.a.active)
	}
	// Opening a change a tab has shows that tab.
	e.a.Update(tabMsg{id: e.a.ids[1], msg: newTabMsg{svc: e.svc, change: e.chg}})
	if len(e.a.tabs) != 2 || e.a.active != 0 {
		t.Errorf("opening a change already open made %d tabs, shows %d; want the first shown", len(e.a.tabs), e.a.active)
	}
	// A click on a tab's label shows it.
	e.a.Update(tea.MouseClickMsg{X: e.a.bar[1].x, Y: 0, Button: tea.MouseLeft})
	if e.a.active != 1 {
		t.Errorf("a click on the second tab's label showed tab %d", e.a.active)
	}

	e.press("ctrl+w")
	if len(e.a.tabs) != 1 || e.a.active != 0 || e.bar() != " #1 feature" {
		t.Errorf("ctrl+w left %d tabs, showing %d, bar %q", len(e.a.tabs), e.a.active, e.bar())
	}
	_, cmd := e.a.Update(keyMsg("ctrl+w"))
	if msg, _ := cmd().(tabMsg); msg.msg != (closeTabMsg{}) {
		t.Fatalf("ctrl+w handed the app %#v", msg)
	}
	_, cmd = e.a.Update(cmd())
	if cmd == nil || cmd() != (tea.QuitMsg{}) {
		t.Error("closing the last tab did not quit")
	}
	// Bubble Tea draws once more before it quits.
	e.a.View()
}
