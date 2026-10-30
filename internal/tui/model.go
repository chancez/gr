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
	focusNav
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

type navItem struct {
	label string
	depth int
	view  int
	// file is the file index in the view, or -1 for the view itself.
	file int
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
	nav     []navItem
	navPos  int
	navTop  int
	main    pane
	split   bool
	focus   focus
	mode    mode
	compose composer

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
		watcher: w, version: version, main: pane{anchor: -1, side: anchor.SideNew},
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
	navWidth = min(max(m.width/4, 24), 48)
	if m.width < 60 {
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

// relayout recomputes row heights after a size change.
func (m *model) relayout(resized bool) {
	_, mw, bh := m.layoutSizes()
	m.main.height = bh
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
		m.main.rows = buildRows(m.data, v, m.split)
	}
	m.main.anchor = -1
	_, mw, bh := m.layoutSizes()
	m.main.width, m.main.height = mw, bh
	m.main.layout(m.renderMainRow)
}

func (m *model) renderMainRow(i int, state rowState) []string {
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

func (m *model) viewLabel(v view) string {
	switch v.kind {
	case viewOverview:
		return "Overview"
	case viewDiff:
		return fmt.Sprintf("Files changed (%d)", len(v.files))
	case viewCommit:
		return shortSHA(v.commit.SHA)[:7] + " " + v.commit.Subject()
	case viewInterdiff:
		return fmt.Sprintf("Since revision %d", m.data.interdiffFrom.Seq)
	}
	return ""
}

// openThreads counts unresolved current threads per file path in a view.
func (m *model) openThreads(v view) map[string]int {
	counts := map[string]int{}
	for i := range m.data.threads {
		t := &m.data.threads[i]
		if t.State != store.ThreadResolved && threadInView(t, v) {
			counts[t.Location.Path]++
		}
	}
	return counts
}

func (m *model) buildNav() {
	if m.data == nil {
		return
	}
	var keep navItem
	if m.navPos < len(m.nav) {
		keep = m.nav[m.navPos]
	}
	m.nav = nil
	for vi, v := range m.views {
		m.nav = append(m.nav, navItem{label: m.viewLabel(v), view: vi, file: -1})
		if vi != m.current || v.kind == viewOverview {
			continue
		}
		counts := m.openThreads(v)
		for fi, f := range v.files {
			label := statusLetter(f.Status) + " " + fileLabel(f)
			if n := counts[f.Path()]; n > 0 {
				label += fmt.Sprintf(" (%d)", n)
			}
			m.nav = append(m.nav, navItem{label: label, depth: 1, view: vi, file: fi})
		}
	}
	m.navPos = 0
	for i, it := range m.nav {
		if it.view == keep.view && it.file == keep.file {
			m.navPos = i
		}
	}
	if m.views[m.current].key() != m.views[m.nav[m.navPos].view].key() {
		for i, it := range m.nav {
			if it.view == m.current && it.file == -1 {
				m.navPos = i
			}
		}
	}
}

// openNav shows the selected sidebar item.
func (m *model) openNav() {
	if m.navPos >= len(m.nav) {
		return
	}
	it := m.nav[m.navPos]
	m.setView(it.view)
	if it.file >= 0 {
		for i, r := range m.main.rows {
			if r.kind == rowFile && r.file == it.file {
				m.main.jump(i)
				break
			}
		}
	}
	// setView rebuilt the nav; find the item again.
	for i, n := range m.nav {
		if n.view == it.view && n.file == it.file {
			m.navPos = i
		}
	}
}

// syncNav highlights the sidebar file under the main cursor.
func (m *model) syncNav() {
	if m.main.cursor >= len(m.main.rows) {
		return
	}
	file := m.main.rows[m.main.cursor].file
	for i, it := range m.nav {
		if it.view == m.current && it.file == file {
			m.navPos = i
			return
		}
	}
	for i, it := range m.nav {
		if it.view == m.current && it.file == -1 {
			m.navPos = i
		}
	}
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
		if m.focus == focusMain {
			m.focus = focusNav
		} else {
			m.focus = focusMain
		}
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
	if m.focus == focusNav {
		return m.navKey(msg)
	}
	return m.mainKey(msg)
}

func (m *model) navKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	prev := m.navPos
	switch {
	case key.Matches(msg, k.Up):
		m.navPos = max(m.navPos-1, 0)
	case key.Matches(msg, k.Down):
		m.navPos = min(m.navPos+1, len(m.nav)-1)
	case key.Matches(msg, k.Top):
		m.navPos = 0
	case key.Matches(msg, k.Bottom):
		m.navPos = len(m.nav) - 1
	case key.Matches(msg, k.Open), key.Matches(msg, k.Right):
		m.openNav()
		m.focus = focusMain
		return m, nil
	default:
		return m.mainKey(msg)
	}
	if m.navPos != prev {
		m.openNav()
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
		p.side = anchor.SideOld
	case key.Matches(msg, k.Right):
		p.side = anchor.SideNew
	case key.Matches(msg, k.NextFile):
		m.jumpTo(1, func(r row) bool { return r.kind == rowFile })
	case key.Matches(msg, k.PrevFile):
		m.jumpTo(-1, func(r row) bool { return r.kind == rowFile })
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
			m.focus = focusNav
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

func (m *model) mouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	nw, _, _ := m.layoutSizes()
	if msg.X < nw {
		switch msg.Button {
		case tea.MouseWheelUp:
			m.navTop = max(m.navTop-3, 0)
		case tea.MouseWheelDown:
			m.navTop = min(m.navTop+3, max(len(m.nav)-1, 0))
		}
		return m, nil
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		m.main.scroll(-3)
	case tea.MouseWheelDown:
		m.main.scroll(3)
	}
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
	if msg.X < nw {
		i := m.navTop + msg.Y
		if i < len(m.nav) {
			m.navPos = i
			m.openNav()
			m.focus = focusNav
		}
		return m, nil
	}
	i := m.main.rowAt(msg.Y)
	if i < 0 || !m.main.rows[i].selectable() {
		return m, nil
	}
	m.focus = focusMain
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
	mainLines := m.main.visible(m.renderMainRow)
	navLines := m.renderNav(nw, bh)
	sep := m.r.st.paneBorder.Render("|")
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

func (m *model) renderNav(width, height int) []string {
	if width == 0 {
		return nil
	}
	if m.navPos < m.navTop {
		m.navTop = m.navPos
	} else if m.navPos >= m.navTop+height {
		m.navTop = m.navPos - height + 1
	}
	st := m.r.st
	var out []string
	for i := m.navTop; i < len(m.nav) && len(out) < height; i++ {
		it := m.nav[i]
		label := strings.Repeat("  ", it.depth) + it.label
		if it.depth == 0 {
			label = st.navHeader.Render(label)
		}
		line := fit(" "+label, width, nil)
		if i == m.navPos {
			style := st.navSelected
			if m.focus == focusNav {
				style = st.navFocused
			}
			line = style.Render(fit(" "+ansi.Strip(label), width, nil))
		}
		out = append(out, line)
	}
	return out
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
