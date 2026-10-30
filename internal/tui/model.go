// Package tui is gr's terminal interface, where reviews are written.
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// Run opens the TUI on a change.
func Run(ctx context.Context, svc *review.Service, change store.Change) error {
	m, err := newModel(ctx, svc, change)
	if err != nil {
		return err
	}
	defer m.close()
	_, err = tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

type focus int

const (
	focusMain focus = iota
	focusViews
	focusFiles
)

type mode int

const (
	modeNormal mode = iota
	modeCompose
	modeVerdict
	modeConfirmDiscard
	modeHelp
)

type composeKind int

const (
	composeThread composeKind = iota
	composeReply
	composeEdit
	composeSubmit
)

type composer struct {
	kind      composeKind
	title     string
	area      textarea.Model
	thread    review.NewThread
	threadID  string
	commentID string
	verdict   store.Verdict
}

type model struct {
	ctx      context.Context
	svc      *review.Service
	changeID string
	keys     keyMap
	help     help.Model
	r        *renderer

	data    *changeData
	views   []view
	current int
	// viewsNav lists the overview, interdiff, commits, and whole diff. filesNav lists the current
	// view's files.
	viewsNav navList
	filesNav navList
	// navWidth is the sidebar width once dragged, or 0 for the default.
	navWidth int
	dragging bool
	// returnView is where the all-changes key goes back to.
	returnView int
	// expanded overrides whether a file is collapsed, keyed by view and path.
	expanded map[string]bool
	main     pane
	split    bool
	focus    focus
	mode     mode
	compose  composer

	status    string
	statusErr bool
	width     int
	height    int

	watcher *store.Watcher
	version int64
}

type dataMsg struct {
	data *changeData
	err  error
}

type opMsg struct {
	status string
	err    error
}

type tickMsg struct{}

type refreshMsg struct {
	version int64
	changed bool
}

type editorMsg struct {
	text string
	err  error
}

func newModel(ctx context.Context, svc *review.Service, change store.Change) (*model, error) {
	w, err := svc.Store().DataVersion(ctx)
	if err != nil {
		return nil, err
	}
	version, err := w.Version(ctx)
	if err != nil {
		w.Close()
		return nil, err
	}
	m := &model{
		ctx: ctx, svc: svc, changeID: change.ID, keys: defaultKeys(),
		watcher: w, version: version, main: pane{anchor: -1, side: anchor.SideNew, scrolloff: defaultScrolloff}, expanded: map[string]bool{}, returnView: -1,
	}
	// Dark until the terminal reports its background.
	m.setTheme(true)
	return m, nil
}

// setTheme rebuilds colors for a dark or light background.
func (m *model) setTheme(isDark bool) {
	m.r = newRenderer(isDark)
	h := help.New()
	h.ShortSeparator, h.FullSeparator = " | ", "   "
	h.Styles.ShortKey, h.Styles.FullKey = m.r.st.helpKey, m.r.st.helpKey
	h.Styles.ShortDesc, h.Styles.FullDesc = m.r.st.helpDesc, m.r.st.helpDesc
	h.Styles.ShortSeparator, h.Styles.FullSeparator = m.r.st.muted, m.r.st.muted
	h.SetWidth(m.width)
	m.help = h
}

func (m *model) close() {
	if m.watcher != nil {
		m.watcher.Close()
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.loadCmd(), tick(), tea.RequestBackgroundColor)
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) loadCmd() tea.Cmd {
	ctx, svc, id := m.ctx, m.svc, m.changeID
	return func() tea.Msg {
		d, err := load(ctx, svc, id)
		return dataMsg{data: d, err: err}
	}
}

// refreshCmd checks whether another process wrote to the database or the branch moved.
func (m *model) refreshCmd() tea.Cmd {
	ctx, svc, w, last := m.ctx, m.svc, m.watcher, m.version
	var change store.Change
	var head string
	if m.data != nil {
		change, head = m.data.change, m.data.revision.HeadSHA
	}
	return func() tea.Msg {
		v, err := w.Version(ctx)
		if err != nil {
			return refreshMsg{version: last}
		}
		changed := v != last
		if !changed && change.Kind == store.KindBranch && change.State == store.ChangeOpen {
			if sha, err := svc.Git().ResolveCommit(ctx, "refs/heads/"+change.Branch); err == nil && sha != head {
				changed = true
			}
		}
		return refreshMsg{version: v, changed: changed}
	}
}

func (m *model) setStatus(format string, args ...any) {
	m.status, m.statusErr = fmt.Sprintf(format, args...), false
}

func (m *model) setError(err error) {
	m.status, m.statusErr = err.Error(), true
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
		m.relayout(true)
		return m, nil
	case tea.BackgroundColorMsg:
		m.setTheme(msg.IsDark())
		m.relayout(true)
		return m, nil
	case dataMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.applyData(msg.data)
		return m, nil
	case opMsg:
		if msg.err != nil {
			m.setError(msg.err)
		} else if msg.status != "" {
			m.setStatus("%s", msg.status)
		}
		return m, m.loadCmd()
	case tickMsg:
		return m, tea.Batch(m.refreshCmd(), tick())
	case refreshMsg:
		m.version = msg.version
		if msg.changed {
			return m, m.loadCmd()
		}
		return m, nil
	case editorMsg:
		if msg.err != nil {
			m.setError(msg.err)
		} else if m.mode == modeCompose {
			m.compose.area.SetValue(msg.text)
		}
		return m, nil
	case tea.MouseWheelMsg:
		return m.mouseWheel(msg)
	case tea.MouseClickMsg:
		return m.mouseClick(msg)
	case tea.MouseMotionMsg:
		return m.mouseMotion(msg)
	case tea.MouseReleaseMsg:
		m.dragging = false
		return m, nil
	case tea.KeyPressMsg:
		switch m.mode {
		case modeCompose:
			return m.composeKey(msg)
		case modeVerdict:
			return m.verdictKey(msg)
		case modeConfirmDiscard:
			return m.confirmKey(msg)
		case modeHelp:
			m.mode = modeNormal
			return m, nil
		}
		return m.normalKey(msg)
	}
	if m.mode == modeCompose {
		var cmd tea.Cmd
		m.compose.area, cmd = m.compose.area.Update(msg)
		return m, cmd
	}
	return m, nil
}

// applyData swaps in freshly loaded data, keeping the view and cursor where they were.
func (m *model) applyData(d *changeData) {
	prevView := ""
	prevRow := ""
	if m.data != nil && m.current < len(m.views) {
		prevView = m.views[m.current].key()
		if m.main.cursor < len(m.main.rows) {
			prevRow = m.main.rows[m.main.cursor].key(m.views[m.current].files)
		}
	}
	prevOffset := m.main.offset
	m.data = d
	m.views = buildViews(d)
	m.current = 0
	for i, v := range m.views {
		if v.key() == prevView {
			m.current = i
		}
	}
	m.rebuildMain()
	if prevRow != "" {
		files := m.views[m.current].files
		for i, r := range m.main.rows {
			if r.key(files) == prevRow {
				m.main.cursor = i
				m.main.offset = prevOffset
				m.main.clamp()
				m.main.ensureVisible()
				break
			}
		}
	}
	m.buildNav()
}

func (m *model) layoutSizes() (navWidth, mainWidth, bodyHeight int) {
	navWidth = min(max(m.width/6, 18), 32)
	if m.navWidth > 0 {
		navWidth = min(m.navWidth, m.width-20)
	}
	if m.width < 50 {
		navWidth = 0
	}
	mainWidth = m.width - navWidth
	if navWidth > 0 {
		mainWidth--
	}
	bodyHeight = m.height - 1
	if m.mode == modeCompose {
		bodyHeight -= composeHeight + 3
	}
	return navWidth, max(mainWidth, 10), max(bodyHeight, 1)
}

const composeHeight = 8

// mainHeight is the main pane's row area, below its header line.
func mainHeight(bodyHeight int) int { return max(bodyHeight-1, 1) }

// relayout recomputes row heights after a size change.
func (m *model) relayout(resized bool) {
	_, mw, bh := m.layoutSizes()
	m.main.height = mainHeight(bh)
	if resized || m.main.width != mw {
		m.main.width = mw
		if m.data != nil {
			m.main.layout(m.renderMainRow)
		}
	}
	if m.mode == modeCompose {
		m.compose.area.SetWidth(m.width - 4)
		m.compose.area.SetHeight(composeHeight)
	}
	m.main.clamp()
}

func (m *model) rebuildMain() {
	if m.data == nil {
		return
	}
	v := m.views[m.current]
	if v.kind == viewOverview {
		m.main.rows = buildOverview(m.data, m.r)
	} else {
		m.main.rows = buildRows(m.data, v, rowOptions{
			split:     m.split,
			collapsed: func(f int) bool { return m.collapsed(v, f) },
			reviewed:  func(f int) bool { return m.fileReviewed(v, f) },
		})
	}
	m.main.anchor = -1
	_, mw, bh := m.layoutSizes()
	m.main.width, m.main.height = mw, mainHeight(bh)
	m.main.layout(m.renderMainRow)
}

func (m *model) renderMainRow(i int, state rowState) []string {
	state.dim = m.focus != focusMain
	return m.r.renderRow(m.data, m.views[m.current], m.main.rows[i], m.main.width, state)
}

func (m *model) setView(i int) {
	if i == m.current || i < 0 || i >= len(m.views) {
		return
	}
	m.current = i
	m.main.cursor, m.main.offset = 0, 0
	m.rebuildMain()
	m.buildNav()
}

func (m *model) normalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Help):
		m.mode = modeHelp
		return m, nil
	case key.Matches(msg, k.Cancel):
		m.main.anchor = -1
		m.status = ""
		return m, nil
	case key.Matches(msg, k.Focus):
		m.cycleFocus(1)
		return m, nil
	case key.Matches(msg, k.FocusBack):
		m.cycleFocus(-1)
		return m, nil
	case key.Matches(msg, k.Reload):
		m.setStatus("reloading")
		return m, m.loadCmd()
	case key.Matches(msg, k.Split):
		m.split = !m.split
		m.rebuildMain()
		return m, nil
	case key.Matches(msg, k.Submit):
		if m.data == nil {
			return m, nil
		}
		m.mode = modeVerdict
		return m, nil
	case key.Matches(msg, k.Reviewed), key.Matches(msg, k.CommitDone):
		if m.data == nil {
			return m, nil
		}
		return m, m.toggleReviewed(key.Matches(msg, k.CommitDone))
	case key.Matches(msg, k.Discard):
		if _, ok := m.data.draft(); !ok {
			m.setError(errNoDraft)
			return m, nil
		}
		m.mode = modeConfirmDiscard
		return m, nil
	}
	if m.data == nil {
		return m, nil
	}
	switch {
	case key.Matches(msg, k.AllChanges):
		m.toggleAllChanges()
		return m, nil
	case key.Matches(msg, k.NextCommit):
		m.stepCommit(1)
		return m, nil
	case key.Matches(msg, k.PrevCommit):
		m.stepCommit(-1)
		return m, nil
	}
	switch m.focus {
	case focusViews:
		return m.navKey(msg, &m.viewsNav, m.openView)
	case focusFiles:
		return m.navKey(msg, &m.filesNav, m.openFile)
	}
	return m.mainKey(msg)
}

// focusOrder is the panes from left to right. The files list is skipped when the view has none.
func (m *model) focusOrder() []focus {
	if len(m.filesNav.items) == 0 {
		return []focus{focusViews, focusMain}
	}
	return []focus{focusViews, focusFiles, focusMain}
}

// cycleFocus moves focus n panes along focusOrder, wrapping around.
func (m *model) cycleFocus(n int) {
	order := m.focusOrder()
	i := 0
	for j, f := range order {
		if f == m.focus {
			i = j
		}
	}
	m.focus = order[(i+n+len(order))%len(order)]
}

// focusStep moves focus n panes without wrapping, reporting whether it moved.
func (m *model) focusStep(n int) bool {
	order := m.focusOrder()
	for j, f := range order {
		if f == m.focus {
			next := min(max(j+n, 0), len(order)-1)
			m.focus = order[next]
			return next != j
		}
	}
	m.focus = focusMain
	return true
}

func (m *model) navKey(msg tea.KeyPressMsg, l *navList, open func()) (tea.Model, tea.Cmd) {
	k := m.keys
	_, _, bh := m.layoutSizes()
	vh, fh := m.sidebarHeights(bh)
	page := vh - 1
	if l == &m.filesNav {
		page = fh - 1
	}
	changed := false
	switch {
	case key.Matches(msg, k.Up):
		changed = l.move(-1)
	case key.Matches(msg, k.Down):
		changed = l.move(1)
	case key.Matches(msg, k.HalfPageUp):
		changed = l.move(-max(page/2, 1))
	case key.Matches(msg, k.HalfPageDown):
		changed = l.move(max(page/2, 1))
	case key.Matches(msg, k.PageUp):
		changed = l.move(-max(page, 1))
	case key.Matches(msg, k.PageDown):
		changed = l.move(max(page, 1))
	case key.Matches(msg, k.Top):
		changed = l.set(0)
	case key.Matches(msg, k.Bottom):
		changed = l.set(len(l.items) - 1)
	case key.Matches(msg, k.Left):
		m.focusStep(-1)
		return m, nil
	case key.Matches(msg, k.Open), key.Matches(msg, k.Right):
		open()
		m.focusStep(1)
		return m, nil
	default:
		return m.mainKey(msg)
	}
	if changed {
		open()
	}
	return m, nil
}

func (m *model) mainKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	p := &m.main
	switch {
	case key.Matches(msg, k.Up):
		p.move(-1)
	case key.Matches(msg, k.Down):
		p.move(1)
	case key.Matches(msg, k.HalfPageDown):
		p.moveLines(p.height / 2)
	case key.Matches(msg, k.HalfPageUp):
		p.moveLines(-p.height / 2)
	case key.Matches(msg, k.PageDown):
		p.moveLines(p.height)
	case key.Matches(msg, k.PageUp):
		p.moveLines(-p.height)
	case key.Matches(msg, k.Top):
		p.top()
	case key.Matches(msg, k.Bottom):
		p.bottom()
	case key.Matches(msg, k.Left):
		// In split mode, left first moves to the old side, then to the sidebar.
		if m.split && p.side == anchor.SideNew {
			p.side = anchor.SideOld
		} else {
			m.focusStep(-1)
		}
	case key.Matches(msg, k.Right):
		p.side = anchor.SideNew
	case key.Matches(msg, k.NextFile):
		m.jumpTo(1, func(r row) bool { return r.kind == rowFile })
	case key.Matches(msg, k.PrevFile):
		m.jumpTo(-1, func(r row) bool { return r.kind == rowFile })
	case key.Matches(msg, k.NextHunk):
		m.jumpTo(1, func(r row) bool { return r.kind == rowHunk })
	case key.Matches(msg, k.PrevHunk):
		m.jumpTo(-1, func(r row) bool { return r.kind == rowHunk })
	case key.Matches(msg, k.NextThread):
		m.jumpTo(1, func(r row) bool { return r.kind == rowThread })
	case key.Matches(msg, k.PrevThread):
		m.jumpTo(-1, func(r row) bool { return r.kind == rowThread })
	case key.Matches(msg, k.Select):
		if p.anchor >= 0 {
			p.anchor = -1
		} else if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowLine {
			p.anchor = p.cursor
		}
	case key.Matches(msg, k.Open):
		if m.views[m.current].kind == viewOverview {
			m.focus = focusViews
		} else if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowFile {
			m.toggleCollapsed(m.views[m.current], p.rows[p.cursor].file)
		}
	case key.Matches(msg, k.Comment):
		return m.startComment(false)
	case key.Matches(msg, k.FileComment):
		return m.startComment(true)
	case key.Matches(msg, k.Reply):
		return m.threadAction(composeReply)
	case key.Matches(msg, k.Edit):
		return m.threadAction(composeEdit)
	case key.Matches(msg, k.Resolve):
		t, ok := m.cursorThread()
		if !ok {
			return m, nil
		}
		state := store.ThreadResolved
		if t.State == store.ThreadResolved {
			state = store.ThreadOpen
		}
		return m, m.setThreadState(t, state)
	case key.Matches(msg, k.Address):
		t, ok := m.cursorThread()
		if !ok {
			return m, nil
		}
		return m, m.setThreadState(t, store.ThreadAddressed)
	case key.Matches(msg, k.Delete):
		t, ok := m.cursorThread()
		if !ok {
			return m, nil
		}
		c, ok := m.lastOwnComment(t)
		if !ok {
			m.setError(errors.New("no comment of yours in this thread"))
			return m, nil
		}
		svc, ctx := m.svc, m.ctx
		return m, func() tea.Msg {
			return opMsg{status: "comment deleted", err: svc.DeleteComment(ctx, c.ID)}
		}
	default:
		return m, nil
	}
	m.syncNav()
	return m, nil
}

// toggleAllChanges opens the whole diff, or goes back to the view it was opened from.
func (m *model) toggleAllChanges() {
	if m.views[m.current].kind == viewDiff {
		if m.returnView >= 0 && m.returnView < len(m.views) {
			m.viewsNav.pos = m.returnView
			m.openView()
		}
		return
	}
	for i, v := range m.views {
		if v.kind == viewDiff {
			m.returnView = m.current
			m.viewsNav.pos = i
			m.openView()
		}
	}
}

// stepCommit opens the next or previous commit, starting from the first or last when the current
// view is not a commit.
func (m *model) stepCommit(dir int) bool {
	for i := m.current + dir; i >= 0 && i < len(m.views); i += dir {
		if m.views[i].kind == viewCommit {
			m.viewsNav.pos = i
			m.openView()
			return true
		}
	}
	return false
}

func (m *model) jumpTo(dir int, match func(row) bool) {
	p := &m.main
	for i := p.cursor + dir; i >= 0 && i < len(p.rows); i += dir {
		if match(p.rows[i]) {
			p.jump(i)
			return
		}
	}
}

func (m *model) cursorThread() (review.ThreadView, bool) {
	p := &m.main
	if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowThread {
		return *p.rows[p.cursor].thread, true
	}
	m.setError(errors.New("move the cursor to a thread first"))
	return review.ThreadView{}, false
}

func (m *model) lastOwnComment(t review.ThreadView) (store.Comment, bool) {
	for i := len(t.Comments) - 1; i >= 0; i-- {
		if t.Comments[i].Author == m.svc.Author() {
			return t.Comments[i], true
		}
	}
	return store.Comment{}, false
}

func (m *model) setThreadState(t review.ThreadView, state store.ThreadState) tea.Cmd {
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		return opMsg{status: fmt.Sprintf("thread #%d %s", t.Seq, state), err: svc.SetThreadState(ctx, t.ID, state)}
	}
}

// startComment opens the composer for a new thread at the cursor. whole comments on the cursor's
// file, or the commit when the cursor is on the commit header, or the change on the overview.
func (m *model) startComment(whole bool) (tea.Model, tea.Cmd) {
	v := m.views[m.current]
	if v.readOnly() {
		m.setError(errors.New("the interdiff is read-only; comment on the files or commits"))
		return m, nil
	}
	nt, title, err := m.newThreadAt(v, whole)
	if err != nil {
		m.setError(err)
		return m, nil
	}
	return m, m.openComposer(composer{kind: composeThread, title: title, thread: nt}, "")
}

func (m *model) newThreadAt(v view, whole bool) (review.NewThread, string, error) {
	if v.kind == viewOverview {
		return review.NewThread{Scope: store.ScopeChange}, "Comment on the change", nil
	}
	nt := review.NewThread{Scope: store.ScopeDiff}
	where := "the diff"
	if v.kind == viewCommit {
		nt.Scope, nt.Commit = store.ScopeCommit, v.commit.SHA
		where = "commit " + shortSHA(v.commit.SHA)
	}
	p := &m.main
	if p.cursor >= len(p.rows) {
		return nt, "", errors.New("nothing to comment on")
	}
	cur := p.rows[p.cursor]
	if cur.kind == rowCommit || (whole && cur.file < 0) {
		if v.kind != viewCommit {
			return nt, "", errors.New("move to a file to comment")
		}
		return nt, "Comment on " + where, nil
	}
	if cur.file < 0 {
		return nt, "", errors.New("move to a file or line to comment")
	}
	f := v.files[cur.file]
	if cur.kind == rowFile || cur.kind == rowHunk || cur.kind == rowThread || whole {
		nt.Path, nt.Side = f.NewPath, anchor.SideNew
		if f.NewPath == "" {
			nt.Path, nt.Side = f.OldPath, anchor.SideOld
		}
		return nt, fmt.Sprintf("Comment on %s in %s", nt.Path, where), nil
	}

	// Lines: the cursor line decides the side, and the selection gives the range on that side.
	side := m.lineSide(cur)
	nt.Side, nt.Path = side, f.NewPath
	if side == anchor.SideOld {
		nt.Path = f.OldPath
	}
	selStart, selEnd := p.selection()
	for i := selStart; i <= selEnd; i++ {
		r := p.rows[i]
		if r.kind != rowLine {
			continue
		}
		if r.file != cur.file {
			return nt, "", errors.New("a selection must stay within one file")
		}
		n := lineOn(r, side)
		if n == 0 {
			continue
		}
		if nt.StartLine == 0 || n < nt.StartLine {
			nt.StartLine = n
		}
		nt.EndLine = max(nt.EndLine, n)
	}
	if nt.StartLine == 0 {
		return nt, "", errors.New("no lines on that side")
	}
	lines := fmt.Sprintf("%d", nt.StartLine)
	if nt.EndLine != nt.StartLine {
		lines += fmt.Sprintf("-%d", nt.EndLine)
	}
	sideLabel := ""
	if side == anchor.SideOld {
		sideLabel = " (old)"
	}
	return nt, fmt.Sprintf("Comment on %s:%s%s in %s", nt.Path, lines, sideLabel, where), nil
}

// lineSide is the side a line row is commented on: deletions are old, everything else new. In split
// mode it follows the cursor side, falling back to whichever side has a line.
func (m *model) lineSide(r row) anchor.Side {
	if r.line != nil {
		if r.line.Kind == git.LineDeleted {
			return anchor.SideOld
		}
		return anchor.SideNew
	}
	if m.main.side == anchor.SideOld && r.left != nil || r.right == nil {
		return anchor.SideOld
	}
	return anchor.SideNew
}

func lineOn(r row, side anchor.Side) int {
	old, new := lineNumbers(r)
	if r.line != nil {
		if side == anchor.SideOld && r.line.Kind == git.LineDeleted {
			return old
		}
		if side == anchor.SideNew && r.line.Kind != git.LineDeleted {
			return new
		}
		return 0
	}
	if side == anchor.SideOld {
		return old
	}
	return new
}

func (m *model) threadAction(kind composeKind) (tea.Model, tea.Cmd) {
	t, ok := m.cursorThread()
	if !ok {
		return m, nil
	}
	c := composer{kind: kind, threadID: t.ID}
	initial := ""
	switch kind {
	case composeReply:
		c.title = fmt.Sprintf("Reply to #%d", t.Seq)
	case composeEdit:
		own, ok := m.lastOwnComment(t)
		if !ok {
			m.setError(errors.New("no comment of yours in this thread"))
			return m, nil
		}
		c.title, c.commentID, initial = fmt.Sprintf("Edit your comment on #%d", t.Seq), own.ID, own.Body
	}
	return m, m.openComposer(c, initial)
}

func (m *model) openComposer(c composer, initial string) tea.Cmd {
	c.area = textarea.New()
	c.area.ShowLineNumbers = false
	c.area.Placeholder = "Markdown. ctrl+s to save, ctrl+e for $EDITOR, esc to cancel."
	c.area.SetValue(initial)
	m.compose = c
	m.mode = modeCompose
	m.relayout(false)
	m.main.ensureVisible()
	return m.compose.area.Focus()
}

func (m *model) closeComposer() {
	m.mode = modeNormal
	m.compose = composer{}
	m.relayout(false)
}

func (m *model) composeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	switch {
	case key.Matches(msg, k.Cancel):
		m.closeComposer()
		return m, nil
	case key.Matches(msg, k.ExternalEdit):
		return m, editorCmd(m.compose.area.Value())
	case key.Matches(msg, k.Save):
		return m.saveCompose()
	}
	var cmd tea.Cmd
	m.compose.area, cmd = m.compose.area.Update(msg)
	return m, cmd
}

func (m *model) saveCompose() (tea.Model, tea.Cmd) {
	c := m.compose
	body := strings.TrimSpace(c.area.Value())
	if body == "" && c.kind != composeSubmit {
		m.setError(errors.New("comment is empty"))
		return m, nil
	}
	svc, ctx, change := m.svc, m.ctx, m.data.change
	m.closeComposer()
	m.main.anchor = -1
	return m, func() tea.Msg {
		switch c.kind {
		case composeThread:
			c.thread.Body = body
			t, err := svc.StartThread(ctx, change, c.thread)
			return opMsg{status: fmt.Sprintf("draft thread #%d added; S to submit", t.Seq), err: err}
		case composeReply:
			_, err := svc.Reply(ctx, change, c.threadID, body, false)
			return opMsg{status: "replied", err: err}
		case composeEdit:
			return opMsg{status: "comment updated", err: svc.EditComment(ctx, c.commentID, body)}
		case composeSubmit:
			r, err := svc.Submit(ctx, change, c.verdict, body)
			return opMsg{status: fmt.Sprintf("submitted review #%d: %s", r.Seq, r.Verdict), err: err}
		}
		return nil
	}
}

func (m *model) verdictKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var verdict store.Verdict
	switch msg.String() {
	case "a":
		verdict = store.VerdictApprove
	case "r":
		verdict = store.VerdictRequestChanges
	case "c":
		verdict = store.VerdictComment
	default:
		m.mode = modeNormal
		return m, nil
	}
	initial := ""
	if d, ok := m.data.draft(); ok {
		initial = d.Body
	}
	return m, m.openComposer(composer{kind: composeSubmit, verdict: verdict, title: "Submit review: " + string(verdict) + " (summary is optional)"}, initial)
}

func (m *model) confirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	if msg.String() != "y" {
		return m, nil
	}
	svc, ctx, change := m.svc, m.ctx, m.data.change
	return m, func() tea.Msg {
		return opMsg{status: "draft review discarded", err: svc.Discard(ctx, change)}
	}
}

// editorCmd suspends the TUI and edits text in $VISUAL or $EDITOR.
func editorCmd(text string) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	tmp, err := os.CreateTemp("", "gr-*.md")
	if err != nil {
		return func() tea.Msg { return editorMsg{err: err} }
	}
	_, werr := tmp.WriteString(text)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return func() tea.Msg { return editorMsg{err: err} }
	}
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", tmp.Name())
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(tmp.Name())
		if err != nil {
			return editorMsg{err: err}
		}
		b, err := os.ReadFile(tmp.Name())
		return editorMsg{text: strings.TrimRight(string(b), "\n"), err: err}
	})
}

// sidebarAt maps a sidebar line to a list and its item index.
func (m *model) sidebarAt(y int) (*navList, focus, int) {
	_, _, bh := m.layoutSizes()
	vh, _ := m.sidebarHeights(bh)
	if y < vh {
		return &m.viewsNav, focusViews, m.viewsNav.at(y - 1)
	}
	// Past the divider and the files header.
	return &m.filesNav, focusFiles, m.filesNav.at(y - vh - sidebarGap - 1)
}

func (m *model) mouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	nw, _, _ := m.layoutSizes()
	delta := 3
	if msg.Button == tea.MouseWheelUp {
		delta = -3
	} else if msg.Button != tea.MouseWheelDown {
		return m, nil
	}
	if msg.X < nw {
		l, _, _ := m.sidebarAt(msg.Y)
		l.top = max(l.top+delta, 0)
		return m, nil
	}
	m.main.scroll(delta)
	m.syncNav()
	return m, nil
}

func (m *model) mouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeNormal || msg.Button != tea.MouseLeft {
		return m, nil
	}
	nw, mw, bh := m.layoutSizes()
	if msg.Y >= bh {
		return m, nil
	}
	if nw > 0 && msg.X == nw {
		// The divider: drag to resize the sidebar.
		m.dragging = true
		return m, nil
	}
	if msg.X < nw {
		l, f, i := m.sidebarAt(msg.Y)
		m.focus = f
		if i >= 0 {
			l.pos = i
			if f == focusViews {
				m.openView()
			} else {
				m.openFile()
			}
		}
		return m, nil
	}
	m.focus = focusMain
	i := m.main.rowAt(msg.Y - 1)
	if i < 0 || !m.main.rows[i].selectable() {
		return m, nil
	}
	m.main.cursor = i
	if m.split && m.main.rows[i].kind == rowLine {
		if msg.X-nw-1 < mw/2 {
			m.main.side = anchor.SideOld
		} else {
			m.main.side = anchor.SideNew
		}
	}
	m.syncNav()
	return m, nil
}

func (m *model) mouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if !m.dragging {
		return m, nil
	}
	m.navWidth = min(max(msg.X, 12), m.width-20)
	m.relayout(false)
	return m, nil
}

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "gr"
	if m.data != nil {
		v.WindowTitle = fmt.Sprintf("gr #%d %s", m.data.change.Seq, m.data.change.Title)
	}
	return v
}

func (m *model) render() string {
	if m.width == 0 {
		return ""
	}
	if m.mode == modeHelp {
		return m.help.FullHelpView(m.keys.FullHelp())
	}
	if m.data == nil {
		msg := "loading..."
		if m.statusErr {
			msg = m.status
		}
		return msg
	}
	nw, mw, bh := m.layoutSizes()
	mainLines := append([]string{m.mainHeader(mw)}, m.main.visible(m.renderMainRow)...)
	navLines := m.renderSidebar(nw, bh)
	sep := m.r.st.paneBorder.Render("|")
	if m.dragging {
		sep = lipgloss.NewStyle().Foreground(m.r.st.accent).Render("|")
	}
	var b strings.Builder
	for y := range bh {
		if nw > 0 {
			nl := ""
			if y < len(navLines) {
				nl = navLines[y]
			}
			b.WriteString(fit(nl, nw, nil))
			b.WriteString(sep)
		}
		ml := ""
		if y < len(mainLines) {
			ml = mainLines[y]
		}
		b.WriteString(fit(ml, mw, nil))
		b.WriteString("\n")
	}
	if m.mode == modeCompose {
		title := m.r.st.heading.Render(m.compose.title) + m.r.st.muted.Render("  ctrl+s save, ctrl+e $EDITOR, esc cancel")
		box := m.r.st.composeBox.Width(m.width).Render(ansi.Truncate(title, m.width-4, "") + "\n" + m.compose.area.View())
		b.WriteString(box)
		b.WriteString("\n")
	}
	b.WriteString(m.renderStatus())
	return b.String()
}

func (m *model) renderStatus() string {
	st := m.r.st
	switch m.mode {
	case modeVerdict:
		return st.heading.Render("Submit review: ") + "[a]pprove  [r]equest changes  [c]omment  " + st.muted.Render("any other key cancels")
	case modeConfirmDiscard:
		return st.statusErr.Render("Discard your draft review and all its comments? y/n")
	}
	d := m.data
	left := fmt.Sprintf("#%d rev %d", d.change.Seq, d.revision.Seq)
	if d.change.State != store.ChangeOpen {
		left += " " + string(d.change.State)
	}
	if _, ok := d.draft(); ok {
		left += st.badgeWarn.Render(" draft")
	}
	left += " " + m.reviewProgress()
	if m.split {
		left += " split"
	}
	if m.main.anchor >= 0 {
		left += st.badgeWarn.Render(" selecting")
	}
	right := m.help.ShortHelpView(m.keys.ShortHelp())
	if m.status != "" {
		style := st.statusBar
		if m.statusErr {
			style = st.statusErr
		}
		right = style.Render(m.status)
	}
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
	return fit(st.statusBar.Render(left)+strings.Repeat(" ", gap)+right, m.width, nil)
}
