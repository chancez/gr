package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/termtext"
	"github.com/chancez/gr/internal/tui/look"
)

// A tab is a change open in gr, with all of the TUI's state for it: its own model, which knows
// nothing of the others. The app holds the tabs, draws the tab bar above the one shown, and
// routes to each tab what is its: the results of the commands it started come back marked with
// its id, input goes to the tab shown, and what the terminal says goes to all.

// app is gr's tabs, and which is shown.
type app struct {
	ctx    context.Context
	opts   Options
	tabs   []*model
	ids    []int
	active int
	nextID int
	width  int
	height int
	// background and terminal are what the terminal said of itself, told again to new tabs.
	background *tea.BackgroundColorMsg
	terminal   *tea.TerminalVersionMsg
	// bar is where each tab's label was drawn on the tab bar, for a click on it.
	bar []tabLabel
}

// tabLabel is where a tab's label is on the tab bar.
type tabLabel struct {
	x int
	w int
}

// tabMsg is a message for the tab with id: the result of a command it started.
type tabMsg struct {
	id  int
	msg tea.Msg
}

// The tab actions' messages, which a tab hands to the app.
type newTabMsg struct {
	svc    *review.Service
	change store.Change
}

type closeTabMsg struct{}

type stepTabMsg struct {
	dir int
}

type goTabMsg struct {
	n int
}

// tabCmd hands msg to the app.
func tabCmd(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// openTab opens change c in a new tab, as the tab action does.
func (m *model) openTab(c store.Change) tea.Cmd {
	return tabCmd(newTabMsg{svc: m.svc, change: c})
}

func newApp(ctx context.Context, opts Options, first *model) *app {
	a := &app{ctx: ctx, opts: opts}
	a.add(first)
	return a
}

// add makes m a tab, after the one shown, and shows it.
func (a *app) add(m *model) {
	at := min(a.active+1, len(a.tabs))
	if len(a.tabs) == 0 {
		at = 0
	}
	a.tabs = append(a.tabs[:at], append([]*model{m}, a.tabs[at:]...)...)
	a.ids = append(a.ids[:at], append([]int{a.nextID}, a.ids[at:]...)...)
	a.nextID++
	a.active = at
}

// wrap marks what cmd says as tab id's. Bubble Tea's own messages, as running $EDITOR, are left
// for it, and a batch's commands are each marked.
func wrap(id int, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		switch msg := msg.(type) {
		case nil:
			return nil
		case tea.BatchMsg:
			out := make(tea.BatchMsg, len(msg))
			for i, c := range msg {
				out[i] = wrap(id, c)
			}
			return out
		}
		if reflect.TypeOf(msg).PkgPath() == reflect.TypeFor[tea.BatchMsg]().PkgPath() {
			return msg
		}
		return tabMsg{id: id, msg: msg}
	}
}

func (a *app) Init() tea.Cmd {
	return wrap(a.ids[a.active], a.tabs[a.active].Init())
}

// tab is the index of the tab with id, or -1.
func (a *app) tab(id int) int {
	for i, t := range a.ids {
		if t == id {
			return i
		}
	}
	return -1
}

// send gives msg to tab i, and marks what it starts as the tab's.
func (a *app) send(i int, msg tea.Msg) tea.Cmd {
	_, cmd := a.tabs[i].Update(msg)
	return wrap(a.ids[i], cmd)
}

// all gives msg to every tab.
func (a *app) all(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(a.tabs))
	for i := range a.tabs {
		cmds[i] = a.send(i, msg)
	}
	return tea.Batch(cmds...)
}

// tabSize is the size a tab has, under the tab bar.
func (a *app) tabSize() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: a.width, Height: max(a.height-1, 1)}
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tabMsg:
		i := a.tab(msg.id)
		if i < 0 {
			// From a tab closed since.
			return a, nil
		}
		switch inner := msg.msg.(type) {
		case newTabMsg:
			return a, a.open(inner)
		case closeTabMsg:
			return a, a.close(i)
		case stepTabMsg:
			a.show(a.active + inner.dir)
			return a, nil
		case goTabMsg:
			if inner.n < len(a.tabs) {
				a.show(inner.n)
			}
			return a, nil
		}
		return a, a.send(i, msg.msg)
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, a.all(a.tabSize())
	case tea.BackgroundColorMsg:
		a.background = &msg
		return a, a.all(msg)
	case tea.TerminalVersionMsg:
		a.terminal = &msg
		return a, a.all(msg)
	case tea.MouseClickMsg:
		if msg.Y == 0 {
			a.clickBar(msg)
			return a, nil
		}
		msg.Y--
		return a, a.send(a.active, msg)
	case tea.MouseReleaseMsg:
		msg.Y--
		return a, a.send(a.active, msg)
	case tea.MouseMotionMsg:
		msg.Y--
		return a, a.send(a.active, msg)
	case tea.MouseWheelMsg:
		msg.Y--
		return a, a.send(a.active, msg)
	}
	return a, a.send(a.active, msg)
}

// show shows tab i, wrapping around the ends.
func (a *app) show(i int) {
	n := len(a.tabs)
	a.active = ((i % n) + n) % n
}

// open opens a change in a new tab, after the one shown, or shows it where a tab has it already.
func (a *app) open(msg newTabMsg) tea.Cmd {
	for i, t := range a.tabs {
		if t.changeID == msg.change.ID {
			a.show(i)
			return nil
		}
	}
	m, err := newTab(a.ctx, msg.svc, msg.change, a.opts)
	if err != nil {
		cur := a.tabs[a.active]
		cur.setError(err)
		return nil
	}
	a.add(m)
	id := a.ids[a.active]
	cmds := []tea.Cmd{wrap(id, m.Init()), a.send(a.active, a.tabSize())}
	// What the terminal said of itself, which the new tab asks again but would hear only as the
	// app's.
	if a.background != nil {
		cmds = append(cmds, a.send(a.active, *a.background))
	}
	if a.terminal != nil {
		cmds = append(cmds, a.send(a.active, *a.terminal))
	}
	return tea.Batch(cmds...)
}

// close closes tab i, saving where it was. Closing the last quits, keeping it for Bubble Tea's last
// frame, as quitting saves where it was.
func (a *app) close(i int) tea.Cmd {
	if len(a.tabs) == 1 {
		return tea.Quit
	}
	t := a.tabs[i]
	t.savePlace()
	t.close()
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	a.ids = append(a.ids[:i], a.ids[i+1:]...)
	if a.active >= i && a.active > 0 {
		a.active--
	}
	return nil
}

// quit saves where every tab was and lets each go.
func (a *app) quit() {
	for _, t := range a.tabs {
		t.savePlace()
		t.close()
	}
}

// clickBar shows the tab whose label a click on the tab bar lands on.
func (a *app) clickBar(msg tea.MouseClickMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}
	for i, l := range a.bar {
		if msg.X >= l.x && msg.X < l.x+l.w {
			a.show(i)
		}
	}
}

func (a *app) View() tea.View {
	if len(a.tabs) == 0 {
		return tea.NewView("")
	}
	v := a.tabs[a.active].View()
	v.Content = a.tabBar() + "\n" + v.Content
	if v.Cursor != nil {
		v.Cursor.Y++
	}
	return v
}

// tabLabel names a tab: its change, and its repository where the tabs are of more than one.
func (a *app) label(m *model, repos bool) string {
	name := fmt.Sprintf("#%d", m.changeSeq())
	if m.data != nil {
		c := m.data.change
		title := c.Title
		if c.Kind == store.KindBranch {
			title = c.Branch
		}
		name += " " + termtext.Line(title)
	}
	if repos {
		name = filepath.Base(m.svc.Repo().LastPath) + " " + name
	}
	return name
}

// tabBar draws the tabs, the one shown in the focused pane's color, each as wide as its label up
// to a share of the width.
func (a *app) tabBar() string {
	th := a.tabs[a.active].r.th
	repos := false
	for _, t := range a.tabs {
		repos = repos || t.svc.Repo().ID != a.tabs[0].svc.Repo().ID
	}
	share := max(a.width/len(a.tabs), 12)
	a.bar = a.bar[:0]
	var b strings.Builder
	x := 0
	for i, t := range a.tabs {
		surf := look.PaneUnfocused
		if i == a.active {
			surf = look.PaneFocused
		}
		label := " " + a.label(t, repos) + " "
		w := min(ansi.StringWidth(label), share, max(a.width-x, 0))
		if w <= 0 {
			break
		}
		b.WriteString(th.DrawCut(surf, w, look.Text(label)))
		a.bar = append(a.bar, tabLabel{x: x, w: w})
		x += w
		if x < a.width {
			b.WriteString(" ")
			x++
		}
	}
	return look.Fit(b.String(), a.width)
}
