package tui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
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
		case newTabMsg, closeTabMsg, stepTabMsg, goTabMsg, moveTabMsg:
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
	if got, want := e.bar(), " #1 feature x   #2 other x   +"; e.a.active != 1 || got != want {
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
	if len(e.a.tabs) != 1 || e.a.active != 0 || e.bar() != " #1 feature x   +" {
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

// ctrl+t lists the open changes of the other repositories gr has reviewed, by repository, and
// opens one in a tab of its own, its repository named on the tab bar.
func TestTabsOfOtherRepos(t *testing.T) {
	e := newTabsEnv(t)
	other := gittest.New(t)
	other.Write("x.txt", "x\n")
	other.Commit("root")
	other.Git("checkout", "-q", "-b", "elsewhere")
	other.Write("x.txt", "y\n")
	other.Commit("change x")
	open := func(ctx context.Context, dir string) (*review.Service, error) {
		g, err := git.Open(ctx, dir)
		if err != nil {
			return nil, err
		}
		return review.Open(ctx, e.store, g, e.svc.Author())
	}
	svc, err := open(t.Context(), other.Dir)
	if err != nil {
		t.Fatal(err)
	}
	target, err := svc.ParseTarget(t.Context(), "elsewhere", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.EnsureChange(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	e.a.tabs[0].openRepo = open

	e.press("ctrl+t")
	tab := e.a.tabs[0]
	if tab.mode != modePicker {
		t.Fatalf("ctrl+t left mode %d", tab.mode)
	}
	var names []string
	for _, it := range tab.picker.items {
		names = append(names, it.value)
	}
	if !slices.Contains(names, filepath.Base(other.Dir)) || !slices.Contains(names, "#1 elsewhere onto main") {
		t.Fatalf("the new tab picker lists %q, want the other repository and its change", names)
	}
	(&tuiEnv{t: t, m: tab}).typeText("elsewhere onto")
	_, cmd := e.a.Update(keyMsg("enter"))
	msg := cmd().(tabMsg)
	e.a.Update(msg)
	if len(e.a.tabs) != 2 || e.a.tabs[1].svc.Repo().ID == e.svc.Repo().ID {
		t.Fatalf("picking the other repository's change made %d tabs", len(e.a.tabs))
	}
	if bar := e.bar(); !strings.Contains(bar, filepath.Base(e.repo.Dir)+" #1") || !strings.Contains(bar, filepath.Base(other.Dir)+" #1") {
		t.Errorf("tab bar %q does not name the repositories", bar)
	}
}

// The tab bar's buttons and keys: + opens the picker of changes, a tab's close button closes it,
// dragging a tab's label moves it, and ctrl+shift+pgup and pgdown move the tab shown.
func TestTabBarControls(t *testing.T) {
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
	e.a.tabBar()
	order := func() []string {
		var out []string
		for _, t := range e.a.tabs {
			out = append(out, t.shownChange().Branch)
		}
		return out
	}

	e.press("ctrl+shift+pgup")
	if got, want := order(), []string{"other", "feature"}; !reflect.DeepEqual(got, want) || e.a.active != 0 {
		t.Errorf("ctrl+shift+pgup: tabs %q, showing %d; want %q, showing the moved tab", got, e.a.active, want)
	}
	e.press("ctrl+shift+pgdown")
	if got, want := order(), []string{"feature", "other"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ctrl+shift+pgdown: tabs %q, want %q", got, want)
	}

	// Drag the second tab onto the first.
	e.a.tabBar()
	from, to := e.a.bar[1], e.a.bar[0]
	e.a.Update(tea.MouseClickMsg{X: from.x + 1, Y: 0, Button: tea.MouseLeft})
	e.a.Update(tea.MouseMotionMsg{X: to.x + 1, Y: 0, Button: tea.MouseLeft})
	e.a.Update(tea.MouseReleaseMsg{X: to.x + 1, Y: 0, Button: tea.MouseLeft})
	if got, want := order(), []string{"other", "feature"}; !reflect.DeepEqual(got, want) || e.a.active != 0 || e.a.drag != -1 {
		t.Errorf("after dragging: tabs %q, showing %d, drag %d; want %q, showing the dragged tab", got, e.a.active, e.a.drag, want)
	}

	// + opens the picker of changes in the tab shown.
	e.a.tabBar()
	e.a.Update(tea.MouseClickMsg{X: e.a.newTab.x + 1, Y: 0, Button: tea.MouseLeft})
	if m := e.a.tabs[e.a.active]; m.mode != modePicker || m.picker.title != "Open in a new tab" {
		t.Errorf("+ left mode %d, picker %q", m.mode, m.picker.title)
	}
	e.a.tabs[e.a.active].closePicker()

	// A tab's close button closes it.
	e.a.tabBar()
	e.a.Update(tea.MouseClickMsg{X: e.a.bar[0].closeX, Y: 0, Button: tea.MouseLeft})
	if got, want := order(), []string{"feature"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after the first tab's close button: tabs %q, want %q", got, want)
	}
}

// Any repository opens by its path: ctrl+t's last entry asks for one, then lists its changes and
// branches, under its name and path, and the one picked opens in a new tab.
func TestOpenARepositoryByPath(t *testing.T) {
	e := newTabsEnv(t)
	other := gittest.New(t)
	other.Write("x.txt", "x\n")
	other.Commit("root")
	other.Git("branch", "fresh")
	e.a.tabs[0].openRepo = func(ctx context.Context, dir string) (*review.Service, error) {
		g, err := git.Open(ctx, dir)
		if err != nil {
			return nil, err
		}
		return review.Open(ctx, e.store, g, e.svc.Author())
	}
	tab := e.a.tabs[0]
	pick := func(text string) {
		t.Helper()
		(&tuiEnv{t: t, m: tab}).typeText(text)
		_, cmd := e.a.Update(keyMsg("enter"))
		if cmd == nil {
			return
		}
		if msg, ok := cmd().(tabMsg); ok {
			e.a.Update(msg)
		}
	}
	e.press("ctrl+t")
	pick("by its path")
	if tab.mode != modePicker || tab.picker.typed == nil {
		t.Fatalf("picking open by path left mode %d, picker %+v", tab.mode, tab.picker)
	}
	pick(other.Dir)
	if tab.mode != modePicker || len(tab.picker.items) == 0 || tab.picker.items[0].note != other.Dir {
		t.Fatalf("after the path, the picker is %+v; want the repository's entries under its path", tab.picker)
	}
	pick("fresh")
	if len(e.a.tabs) != 2 || e.a.tabs[1].shownChange().Branch != "fresh" {
		t.Errorf("picking the branch made %d tabs, the new on %q", len(e.a.tabs), e.a.tabs[len(e.a.tabs)-1].shownChange().Branch)
	}
}

// The path prompt suggests the directories what is typed completes to, notes the repositories
// among them, and tab completes to the one chosen.
func TestPathCompletion(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"alpha/.git", "alphabet", ".hidden", "beta"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	type entry struct {
		Value string
		Note  string
	}
	got := func(text string) []entry {
		var out []entry
		for _, it := range pathSuggestions(text) {
			out = append(out, entry{it.value, it.note})
		}
		return out
	}
	if g, want := got(root+"/al"), []entry{{root + "/alpha/", "repository"}, {root + "/alphabet/", ""}}; !reflect.DeepEqual(g, want) {
		t.Errorf("suggestions for al = %+v, want %+v", g, want)
	}
	if g, want := got(root+"/"), []entry{{root + "/alpha/", "repository"}, {root + "/alphabet/", ""}, {root + "/beta/", ""}}; !reflect.DeepEqual(g, want) {
		t.Errorf("suggestions under the directory = %+v, want %+v, without the hidden one", g, want)
	}
	if g, want := got(root+"/.h"), []entry{{root + "/.hidden/", ""}}; !reflect.DeepEqual(g, want) {
		t.Errorf("suggestions for .h = %+v, want %+v", g, want)
	}

	e := newTabsEnv(t)
	tab := e.a.tabs[0]
	tab.openRepo = func(context.Context, string) (*review.Service, error) { return nil, os.ErrNotExist }
	tab.pickRepoPath()
	(&tuiEnv{t: t, m: tab}).typeText(root + "/alphab")
	tab.Update(keyMsg("tab"))
	if v := tab.picker.filter.Value(); v != root+"/alphabet/" {
		t.Errorf("tab completed to %q, want %q", v, root+"/alphabet/")
	}
}

// Other repositories' changes that landed since gr last looked go from the new tab picker once
// their git is read, in the background; one gr cannot open stays as listed.
func TestTabPickerSeesOtherReposLanded(t *testing.T) {
	e := newTabsEnv(t)
	open := func(ctx context.Context, dir string) (*review.Service, error) {
		g, err := git.Open(ctx, dir)
		if err != nil {
			return nil, err
		}
		return review.Open(ctx, e.store, g, e.svc.Author())
	}
	repo := func(branches ...string) *gittest.Repo {
		r := gittest.New(t)
		// A root of its own: gr knows a repository by its root commit.
		r.Write("x.txt", branches[0]+"\n")
		r.Commit("root")
		svc, err := open(t.Context(), r.Dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range branches {
			r.Git("checkout", "-q", "-b", b, "main")
			r.Write(b+".txt", b+"\n")
			r.Commit("add " + b)
			target, err := svc.ParseTarget(t.Context(), b, "main")
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := svc.EnsureChange(t.Context(), target); err != nil {
				t.Fatal(err)
			}
		}
		return r
	}
	other := repo("landed", "kept")
	other.Git("checkout", "-q", "main")
	other.Git("merge", "-q", "--ff-only", "landed")
	gone := repo("lost")
	if err := os.RemoveAll(gone.Dir); err != nil {
		t.Fatal(err)
	}

	tab := e.a.tabs[0]
	tab.openRepo = open
	cmd := tab.pickTab()
	names := func() []string {
		var out []string
		for _, it := range tab.picker.items {
			if it.key != "" {
				out = append(out, it.full)
			}
		}
		return out
	}
	otherName, goneName := filepath.Base(other.Dir), filepath.Base(gone.Dir)
	before := []string{goneName + ": #1 lost onto main", otherName + ": #2 kept onto main", otherName + ": #1 landed onto main"}
	slices.Sort(before)
	got := names()
	slices.Sort(got)
	if diff := cmp.Diff(before, got); diff != "" {
		t.Fatalf("as opened (-want +got):\n%s", diff)
	}
	for _, c := range cmd().(tea.BatchMsg) {
		if c == nil {
			continue
		}
		if msg, ok := c().(landedMsg); ok {
			tab.Update(msg)
		}
	}
	after := []string{goneName + ": #1 lost onto main", otherName + ": #2 kept onto main"}
	slices.Sort(after)
	got = names()
	slices.Sort(got)
	if diff := cmp.Diff(after, got); diff != "" {
		t.Errorf("after the check (-want +got):\n%s", diff)
	}
}
