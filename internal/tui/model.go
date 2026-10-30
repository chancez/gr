// Package tui is gr's terminal interface, where reviews are written.
package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// Options configure the TUI.
type Options struct {
	// Theme is a built-in palette, one of Themes. Empty is ThemeDefault.
	Theme string
}

// Run opens the TUI on a change.
func Run(ctx context.Context, svc *review.Service, change store.Change, opts Options) error {
	if opts.Theme != "" && !slices.Contains(Themes, opts.Theme) {
		return fmt.Errorf("unknown theme %q, want one of %s", opts.Theme, strings.Join(Themes, ", "))
	}
	m, err := newModel(ctx, svc, change)
	if err != nil {
		return err
	}
	m.theme = opts.Theme
	m.setTheme(true)
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
	modeConfirm
	modeHelp
	modeSearch
)

type composeKind int

const (
	composeThread composeKind = iota
	composeReply
	composeEdit
	composeSubmit
)

type composer struct {
	kind  composeKind
	title string
	// warn shows the title as a warning.
	warn      bool
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
	// search is the last search pattern, and searchInput the prompt while typing one. searchPos
	// and searchHits are the current match's position among the matches.
	search      string
	searchPos   int
	searchHits  int
	searchInput textinput.Model
	// theme is the built-in palette, and isDark whether the terminal's background is dark.
	theme  string
	isDark bool
	// autoAdvance moves on to the next unreviewed file or commit after marking one reviewed. It is
	// off by default and toggled for the session.
	autoAdvance bool
	// confirm is the action waiting on a y/n prompt.
	confirm confirmation
	// filter selects the threads the overview shows.
	filter threadFilter
	// lastSidebar is the sidebar section h returns to.
	lastSidebar focus
	// viewsHeight is the commit list's height once its divider is dragged, or 0 for the default.
	viewsHeight int
	// draggingSplit is set while the divider between the sidebar lists is dragged.
	draggingSplit bool
	// pendingLoads are views whose diffs to load once the current message is handled, and
	// syncLoads loads them at once instead, for tests. pendingThread and pendingFile are where to go
	// once the current view's diff arrives.
	pendingLoads  []int
	syncLoads     bool
	pendingThread *review.ThreadView
	pendingFile   string
	// jumps is the jump list and jumpAt the place in it, as in vim. restoring is set while going to
	// a place in it, so that does not record a jump of its own.
	jumps     []jumpPoint
	jumpAt    int
	restoring bool
	// returns is where the keys that open a view, such as the overview's and the whole diff's, go
	// back to, by the kind of view they open.
	returns map[viewKind]jumpPoint
	// segments holds where each overview row's items start, filled as rows render.
	segments map[int][]int
	// threadFolds overrides whether a thread is collapsed, by thread id. Resolved threads are
	// collapsed by default.
	threadFolds map[string]bool
	// openGaps holds the open folds of unchanged lines, keyed by gapKey.
	openGaps map[string]bool
	// contents caches files' lines by revision and path, for open folds.
	contents map[string][]string
	// pending is the first key of a two-key command, such as z in zo.
	pending string
	// expanded overrides whether a file is collapsed, keyed by view and path.
	expanded map[string]bool
	main     pane
	split    bool
	focus    focus
	mode     mode
	compose  composer

	status    string
	statusErr bool
	statusAt  time.Time
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

// refreshMsg reports another process's writes (changed), or the branch moving (headMoved), which
// needs git read again.
type refreshMsg struct {
	version   int64
	changed   bool
	headMoved bool
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
		watcher: w, version: version, main: pane{anchor: -1, side: anchor.SideNew, scrolloff: defaultScrolloff}, expanded: map[string]bool{}, openGaps: map[string]bool{}, threadFolds: map[string]bool{}, contents: map[string][]string{}, returns: map[viewKind]jumpPoint{}, lastSidebar: focusFiles,
	}
	// Dark until the terminal reports its background.
	m.setTheme(true)
	return m, nil
}

// setTheme rebuilds colors for a dark or light background.
func (m *model) setTheme(isDark bool) {
	m.isDark = isDark
	m.r = newRenderer(isDark, m.theme)
	h := help.New()
	h.ShortSeparator, h.FullSeparator, h.Ellipsis = " | ", "   ", "..."
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

// reloadCmd re-reads the database, keeping git's data and the loaded diffs.
func (m *model) reloadCmd() tea.Cmd {
	if m.data == nil {
		return m.loadCmd()
	}
	ctx, svc, old := m.ctx, m.svc, m.data
	return func() tea.Msg {
		start := time.Now()
		d, err := reload(ctx, svc, old)
		slog.Debug("reload database", "duration", time.Since(start), "err", err)
		return dataMsg{data: d, err: err}
	}
}

// needsLoad reports whether a view shows diff contents that are not loaded yet.
func (m *model) needsLoad(v view) bool {
	return v.kind != viewOverview && !m.data.loaded[v.key()]
}

// requestLoad asks for the current view's diff contents, synchronously in tests.
func (m *model) requestLoad() {
	if m.data == nil || !m.needsLoad(m.views[m.current]) {
		return
	}
	if m.syncLoads {
		m.applyView(loadView(m.ctx, m.svc, m.data, m.views[m.current]))
		return
	}
	m.pendingLoads = append(m.pendingLoads, m.current)
}

func (m *model) loadViewCmd(i int) tea.Cmd {
	ctx, svc, d, v := m.ctx, m.svc, m.data, m.views[i]
	return func() tea.Msg {
		start := time.Now()
		msg := loadView(ctx, svc, d, v)
		slog.Debug("load view", "view", v.key(), "duration", time.Since(start), "err", msg.err)
		return msg
	}
}

// applyView puts a loaded view's diff contents on screen, keeping the cursor where it was.
func (m *model) applyView(msg viewLoadedMsg) {
	if msg.err != nil {
		m.setError(msg.err)
		return
	}
	if m.data == nil || msg.head != m.data.revision.HeadSHA {
		return
	}
	m.data.apply(msg)
	key := m.views[m.current].key()
	var cur row
	if m.main.cursor < len(m.main.rows) {
		cur = m.main.rows[m.main.cursor]
	}
	m.views = buildViews(m.data)
	for i, v := range m.views {
		if v.key() == key {
			m.current = i
		}
	}
	if key == msg.key {
		m.rebuildKeeping(cur)
		m.buildNav()
		if m.pendingFile != "" {
			m.openFileByPath(m.pendingFile)
			m.pendingFile = ""
		}
		if t := m.pendingThread; t != nil {
			m.pendingThread = nil
			m.openThread(*t, true)
		}
	}
}

func (m *model) loadCmd() tea.Cmd {
	ctx, svc, id, old := m.ctx, m.svc, m.changeID, m.data
	current := ""
	if m.data != nil && m.current < len(m.views) {
		current = m.views[m.current].key()
	}
	return func() tea.Msg {
		start := time.Now()
		d, err := load(ctx, svc, id)
		if err == nil {
			// Keep what is still valid, and load the view on screen, before showing anything.
			d.carryLoaded(old)
			err = d.preload(ctx, svc, current)
		}
		slog.Debug("reload", "duration", time.Since(start), "err", err)
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
		msg := refreshMsg{version: v, changed: v != last}
		if change.Kind == store.KindBranch && change.State == store.ChangeOpen {
			if sha, err := svc.Git().ResolveCommit(ctx, "refs/heads/"+change.Branch); err == nil && sha != head {
				msg.headMoved = true
			}
		}
		return msg
	}
}

// How long messages stay in the status bar before the key hints come back.
const (
	statusTTL = 5 * time.Second
	errorTTL  = 10 * time.Second
)

func (m *model) setStatus(format string, args ...any) {
	m.status, m.statusErr, m.statusAt = fmt.Sprintf(format, args...), false, time.Now()
}

func (m *model) setError(err error) {
	slog.Warn("tui error", "err", err)
	m.status, m.statusErr, m.statusAt = err.Error(), true, time.Now()
}

// expireStatus clears a message once it has been shown long enough. A pending two-key command
// keeps its prompt.
func (m *model) expireStatus(now time.Time) {
	if m.status == "" || m.pending != "" {
		return
	}
	ttl := statusTTL
	if m.statusErr {
		ttl = errorTTL
	}
	if now.Sub(m.statusAt) >= ttl {
		m.status = ""
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	mdl, cmd := m.update(msg)
	if len(m.pendingLoads) > 0 {
		cmds := []tea.Cmd{cmd}
		for _, i := range m.pendingLoads {
			cmds = append(cmds, m.loadViewCmd(i))
		}
		m.pendingLoads = nil
		return mdl, tea.Batch(cmds...)
	}
	return mdl, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case viewLoadedMsg:
		m.applyView(msg)
		return m, nil
	case opMsg:
		if msg.err != nil {
			m.setError(msg.err)
		} else if msg.status != "" {
			m.setStatus("%s", msg.status)
		}
		// An action changes the database, not git: reload just that.
		return m, m.reloadCmd()
	case tickMsg:
		m.expireStatus(time.Now())
		return m, tea.Batch(m.refreshCmd(), tick())
	case refreshMsg:
		m.version = msg.version
		switch {
		case msg.headMoved:
			return m, m.loadCmd()
		case msg.changed:
			return m, m.reloadCmd()
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
		m.dragging, m.draggingSplit = false, false
		return m, nil
	case tea.KeyPressMsg:
		switch m.mode {
		case modeCompose:
			return m.composeKey(msg)
		case modeVerdict:
			return m.verdictKey(msg)
		case modeConfirm:
			return m.confirmKey(msg)
		case modeHelp:
			m.mode = modeNormal
			return m, nil
		case modeSearch:
			return m.searchKey(msg)
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
	prevY := m.main.cursorY()
	prevSeg := m.main.itemSegment()
	m.data = d
	m.views = buildViews(d)
	m.current = 0
	for i, v := range m.views {
		if v.key() == prevView {
			m.current = i
		}
	}
	m.rebuildMain()
	m.requestLoad()
	if prevRow != "" {
		files := m.views[m.current].files
		for i, r := range m.main.rows {
			if r.key(files) == prevRow {
				m.main.placeAt(i, prevSeg, prevY)
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
	m.main.reader = v.kind == viewOverview
	m.main.marker = m.r.hunkMarker(rowState{activeHunk: true})
	m.segments = map[int][]int{}
	m.main.segments = func(i int) []int { return m.segments[i] }
	if v.kind == viewOverview {
		m.main.rows = buildOverview(m.data, m.r, m.filter)
	} else {
		m.main.rows = buildRows(m.data, v, rowOptions{
			split:     m.split,
			collapsed: func(f int) bool { return m.collapsed(v, f) },
			reviewed:  func(f int) bool { return m.fileReviewed(v, f) },
			gapOpen:   func(f, g int) bool { return m.openGaps[gapKey(v, v.files[f].Path(), g)] },
			content:   func(f int) []string { return m.fileContent(v, f) },
		})
	}
	for i := range m.main.rows {
		if t := m.main.rows[i].thread; t != nil && (m.main.rows[i].kind == rowThread || m.main.rows[i].kind == rowReview) {
			m.main.rows[i].collapsed = m.threadCollapsed(*t)
		}
	}
	m.main.anchor = -1
	_, mw, bh := m.layoutSizes()
	m.main.width, m.main.height = mw, mainHeight(bh)
	m.main.layout(m.renderMainRow)
}

func (m *model) renderMainRow(i int, state rowState) []string {
	state.dim = m.focus != focusMain
	width := m.main.width
	if m.main.reader {
		// One column is the margin that marks the selected item.
		width--
	}
	lines := m.r.renderRow(m.data, m.views[m.current], m.main.rows[i], width, state)
	if m.r.segments != nil {
		m.segments[i] = m.r.segments
	}
	return lines
}

func (m *model) setView(i int) {
	if i == m.current || i < 0 || i >= len(m.views) {
		return
	}
	if m.data != nil && !m.restoring && m.current < len(m.views) {
		m.pushJump(m.here())
	}
	m.current = i
	m.main.cursor, m.main.offset = 0, 0
	m.rebuildMain()
	m.buildNav()
	m.requestLoad()
}

func (m *model) normalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	if m.pending != "" {
		return m.pendingKey(msg)
	}
	switch {
	case key.Matches(msg, k.Quit):
		return m, tea.Quit
	case key.Matches(msg, k.Help):
		m.mode = modeHelp
		return m, nil
	case key.Matches(msg, k.Cancel):
		m.main.anchor = -1
		m.status = ""
		m.search, m.searchPos, m.searchHits = "", 0, 0
		return m, nil
	case key.Matches(msg, k.Focus):
		m.cycleFocus(1)
		return m, nil
	case key.Matches(msg, k.FocusBack):
		m.cycleFocus(-1)
		return m, nil
	case key.Matches(msg, k.SectionDown):
		m.focusSection(1)
		return m, nil
	case key.Matches(msg, k.SectionUp):
		m.focusSection(-1)
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
	case key.Matches(msg, k.AutoAdvance):
		m.autoAdvance = !m.autoAdvance
		if m.autoAdvance {
			m.setStatus("auto-advance on: marking reviewed moves to the next unreviewed file")
		} else {
			m.setStatus("auto-advance off: marking reviewed stays put")
		}
		return m, nil
	case key.Matches(msg, k.Reviewed), key.Matches(msg, k.CommitDone):
		if m.data == nil {
			return m, nil
		}
		return m, m.toggleReviewed(key.Matches(msg, k.CommitDone))
	case key.Matches(msg, k.Discard):
		if m.data == nil {
			return m, nil
		}
		return m, m.deleteReviewAtCursor()
	}
	if m.data == nil {
		return m, nil
	}
	switch {
	case key.Matches(msg, k.Fold):
		if m.data != nil && m.views[m.current].kind != viewOverview {
			m.pending = "z"
			m.setStatus("z: o open, c close, a toggle, R open all, M close all")
		}
		return m, nil
	case key.Matches(msg, k.Search):
		if m.data == nil {
			return m, nil
		}
		return m, m.openSearch()
	case key.Matches(msg, k.SearchNext):
		if m.data != nil {
			m.searchStep(1)
		}
		return m, nil
	case key.Matches(msg, k.SearchPrev):
		if m.data != nil {
			m.searchStep(-1)
		}
		return m, nil
	case key.Matches(msg, k.JumpBack):
		if m.data != nil {
			m.jumpBack()
		}
		return m, nil
	case key.Matches(msg, k.JumpForward):
		if m.data != nil {
			m.jumpForward()
		}
		return m, nil
	case key.Matches(msg, k.AllChanges):
		m.toggleAllChanges()
		return m, nil
	case key.Matches(msg, k.Overview):
		m.toggleView(viewOverview)
		return m, nil
	case key.Matches(msg, k.NextUnreviewed):
		m.stepUnreviewed(1)
		return m, nil
	case key.Matches(msg, k.PrevUnreviewed):
		m.stepUnreviewed(-1)
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
	m.setFocus(order[(i+n+len(order))%len(order)])
}

// setFocus focuses a pane, remembering the sidebar section so h returns to it.
func (m *model) setFocus(f focus) {
	if f == focusFiles && len(m.filesNav.items) == 0 {
		f = focusViews
	}
	if f == focusViews || f == focusFiles {
		m.lastSidebar = f
	}
	m.focus = f
}

// focusSidebar focuses the sidebar section used last, the file list by default.
func (m *model) focusSidebar() { m.setFocus(m.lastSidebar) }

// focusSection moves between the stacked sidebar sections.
func (m *model) focusSection(dir int) {
	switch {
	case m.focus == focusViews && dir > 0:
		m.setFocus(focusFiles)
	case m.focus == focusFiles && dir < 0:
		m.setFocus(focusViews)
	}
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
		return m, nil
	case key.Matches(msg, k.Open), key.Matches(msg, k.Right):
		open()
		m.setFocus(focusMain)
		return m, nil
	case key.Matches(msg, k.Comment), key.Matches(msg, k.FileComment):
		return m.sidebarComment()
	default:
		return m.mainKey(msg)
	}
	if changed {
		open()
	}
	return m, nil
}

// sidebarComment comments on what the sidebar has selected: the commit, the file, or, on the
// overview, the change. It moves the main cursor there first, so the comment lands where the
// selection is rather than wherever the diff's cursor was.
func (m *model) sidebarComment() (tea.Model, tea.Cmd) {
	v := m.views[m.current]
	switch {
	case v.kind == viewOverview:
		return m.startComment(false)
	case m.focus == focusFiles:
		m.openFile()
		return m.startComment(true)
	case v.kind == viewCommit:
		for i, r := range m.main.rows {
			if r.kind == rowCommit {
				m.main.cursor = i
			}
		}
		return m.startComment(true)
	}
	m.setError(errors.New("select a commit or file to comment on"))
	return m, nil
}

func (m *model) mainKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	p := &m.main
	if p.reader {
		switch {
		case key.Matches(msg, k.Up):
			p.readerPrev()
			return m, nil
		case key.Matches(msg, k.Down):
			p.readerNext()
			return m, nil
		case key.Matches(msg, k.HalfPageDown):
			p.readerScroll(p.height / 2)
			return m, nil
		case key.Matches(msg, k.HalfPageUp):
			p.readerScroll(-p.height / 2)
			return m, nil
		case key.Matches(msg, k.PageDown):
			p.readerScroll(p.height)
			return m, nil
		case key.Matches(msg, k.PageUp):
			p.readerScroll(-p.height)
			return m, nil
		case key.Matches(msg, k.Top):
			m.jump(func() { p.readerSelect(0) })
			return m, nil
		case key.Matches(msg, k.Bottom):
			m.jump(func() { p.readerSelect(len(p.items) - 1) })
			return m, nil
		}
	}
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
		m.jump(p.top)
	case key.Matches(msg, k.Bottom):
		m.jump(p.bottom)
	case key.Matches(msg, k.Left):
		// In split mode, left first moves to the old side, then to the sidebar.
		if m.split && p.side == anchor.SideNew {
			p.side = anchor.SideOld
		} else {
			m.focusSidebar()
		}
	case key.Matches(msg, k.Right):
		p.side = anchor.SideNew
	case key.Matches(msg, k.NextFile):
		m.jumpTo(1, func(r row) bool { return r.kind == rowFile })
	case key.Matches(msg, k.PrevFile):
		m.jumpTo(-1, func(r row) bool { return r.kind == rowFile })
	case key.Matches(msg, k.NextChange):
		m.jumpToIndex(1, m.isChangeStart)
	case key.Matches(msg, k.PrevChange):
		m.jumpToIndex(-1, m.isChangeStart)
	case key.Matches(msg, k.NextHunk):
		m.jumpTo(1, func(r row) bool { return r.kind == rowHunk })
	case key.Matches(msg, k.PrevHunk):
		m.jumpTo(-1, func(r row) bool { return r.kind == rowHunk })
	case key.Matches(msg, k.NextThread):
		m.jump(func() { m.jumpTo(1, func(r row) bool { return r.kind == rowThread }) })
	case key.Matches(msg, k.PrevThread):
		m.jump(func() { m.jumpTo(-1, func(r row) bool { return r.kind == rowThread }) })
	case key.Matches(msg, k.Select):
		if p.anchor >= 0 {
			p.anchor = -1
		} else if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowLine {
			p.anchor = p.cursor
		}
	case key.Matches(msg, k.OpenThread), key.Matches(msg, k.OpenThreadCommit):
		if m.views[m.current].kind == viewOverview && p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowThread {
			m.openThread(*p.rows[p.cursor].thread, key.Matches(msg, k.OpenThread))
		}
	case key.Matches(msg, k.Filter):
		if m.views[m.current].kind == viewOverview {
			m.filter = (m.filter + 1) % filterCount
			m.rebuildMain()
			m.setStatus("showing %s threads", m.filter)
		}
	case key.Matches(msg, k.Open):
		if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowFile {
			m.toggleCollapsed(m.views[m.current], p.rows[p.cursor].file)
		} else if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowGap {
			m.fold(foldOpen)
		} else if p.cursor < len(p.rows) && (p.rows[p.cursor].kind == rowThread || p.rows[p.cursor].kind == rowReview) {
			m.fold(foldToggle)
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
		// In the overview, delete the selected comment when it is yours.
		if seg := m.main.itemSegment(); m.main.reader && seg < len(t.Comments) && t.Comments[seg].Author == m.svc.Author() {
			c, ok = t.Comments[seg], true
		}
		if !ok {
			m.setError(errors.New("no comment of yours in this thread"))
			return m, nil
		}
		svc, ctx := m.svc, m.ctx
		prompt := fmt.Sprintf("Delete your comment on %s? y/n", t.Label())
		status := "comment deleted"
		if len(t.Comments) > 0 && t.Comments[0].ID == c.ID {
			prompt = fmt.Sprintf("Delete thread %s? Its first comment is the thread. y/n", t.Label())
			status = "thread " + t.Label() + " deleted"
		}
		m.ask(prompt, func() tea.Msg {
			return opMsg{status: status, err: svc.DeleteComment(ctx, c.ID)}
		})
		return m, nil
	default:
		return m, nil
	}
	m.syncNav()
	return m, nil
}

// toggleAllChanges opens the whole diff, or goes back to the view it was opened from.
func (m *model) toggleAllChanges() { m.toggleView(viewDiff) }

// toggleView opens the view of a kind, or, from that view, goes back exactly where it was opened
// from: the same view, row, and screen line.
func (m *model) toggleView(kind viewKind) {
	if m.views[m.current].kind == kind {
		if back, ok := m.returns[kind]; ok {
			delete(m.returns, kind)
			m.pushJump(m.here())
			m.restore(back)
		}
		return
	}
	for i, v := range m.views {
		if v.kind == kind {
			m.returns[kind] = m.here()
			m.viewsNav.pos = i
			m.openView()
			return
		}
	}
}

// openThread shows a thread in the commit or diff it is anchored to, with the cursor on it.
//
// With inDiff, it prefers the whole diff, where a thread on a commit shows at the place its code
// is now, so a fix made after the comment shows beside it; it falls back to the thread's commit.
func (m *model) openThread(t review.ThreadView, inDiff bool) {
	if t.Outdated {
		m.setError(errors.New("the code this thread is on changed; it is shown as it was"))
		return
	}
	if inDiff && t.Location.Scope == store.ScopeCommit && t.Location.StartLine > 0 && !m.data.loaded[view{kind: viewDiff}.key()] && !m.syncLoads {
		// Where the thread shows in the whole diff is known once the diff loads.
		for i, v := range m.views {
			if v.kind == viewDiff {
				m.pendingThread = &t
				m.viewsNav.pos = i
				m.openView()
				return
			}
		}
	}
	if inDiff && t.Location.Scope == store.ScopeCommit && t.Location.StartLine > 0 && m.syncLoads {
		for _, v := range m.views {
			if v.kind == viewDiff && m.needsLoad(v) {
				m.data.apply(loadView(m.ctx, m.svc, m.data, v))
				m.views = buildViews(m.data)
			}
		}
	}
	target := -1
	for i, v := range m.views {
		if _, ok := m.data.locationIn(&t, v); ok && (v.kind == viewCommit || v.kind == viewDiff) {
			if target < 0 || (inDiff && v.kind == viewDiff) || (!inDiff && v.kind == viewCommit) {
				target = i
			}
		}
	}
	if target < 0 {
		m.setError(errors.New("this thread is on the whole change"))
		return
	}
	m.viewsNav.pos = target
	m.openView()
	v := m.views[m.current]
	for i, r := range m.main.rows {
		if r.kind == rowThread && r.thread.ID == t.ID {
			m.main.jump(i)
			m.syncNav()
			return
		}
	}
	// Collapsed files hide their threads: open the file and look again.
	for f, file := range v.files {
		if file.Path() == t.Location.Path && m.collapsed(v, f) {
			m.toggleCollapsed(v, f)
			m.openThread(t, inDiff)
			return
		}
	}
}

// stepCommit opens the next or previous commit, starting from the first or last when the current
// view is not a commit.
func (m *model) stepCommit(dir int) bool {
	return m.stepCommitWhere(dir, func(view) bool { return true })
}

// stepUnreviewed opens the next or previous commit not yet marked reviewed.
func (m *model) stepUnreviewed(dir int) {
	if !m.stepCommitWhere(dir, func(v view) bool { return !m.viewReviewed(v) }) {
		way := "after"
		if dir < 0 {
			way = "before"
		}
		m.setStatus("no unreviewed commit %s this one", way)
	}
}

// stepCommitWhere opens the next or previous commit that match accepts.
func (m *model) stepCommitWhere(dir int, match func(view) bool) bool {
	for i := m.current + dir; i >= 0 && i < len(m.views); i += dir {
		if m.views[i].kind == viewCommit && match(m.views[i]) {
			m.viewsNav.pos = i
			m.openView()
			return true
		}
	}
	return false
}

func (m *model) jumpTo(dir int, match func(row) bool) {
	m.jumpToIndex(dir, func(i int) bool { return match(m.main.rows[i]) })
}

func (m *model) jumpToIndex(dir int, match func(i int) bool) {
	p := &m.main
	for i := p.cursor + dir; i >= 0 && i < len(p.rows); i += dir {
		if match(i) {
			p.jump(i)
			return
		}
	}
}

// isChange reports whether a row shows an added or deleted line.
func isChange(r row) bool {
	if r.kind != rowLine {
		return false
	}
	changed := func(l *git.Line) bool { return l != nil && l.Kind != git.LineContext }
	if r.line != nil {
		return changed(r.line)
	}
	return changed(r.left) || changed(r.right)
}

// isChangeStart reports whether row i starts a run of added and deleted lines. Threads between
// changed lines do not split a run.
func (m *model) isChangeStart(i int) bool {
	rows := m.main.rows
	if !isChange(rows[i]) {
		return false
	}
	for j := i - 1; j >= 0; j-- {
		if rows[j].kind == rowThread {
			continue
		}
		return !isChange(rows[j]) || rows[j].hunk != rows[i].hunk
	}
	return true
}

func (m *model) cursorThread() (review.ThreadView, bool) {
	p := &m.main
	if p.cursor < len(p.rows) && p.rows[p.cursor].thread != nil &&
		(p.rows[p.cursor].kind == rowThread || p.rows[p.cursor].kind == rowReview) {
		return *p.rows[p.cursor].thread, true
	}
	if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowReview {
		m.setError(errors.New("this review has no summary"))
		return review.ThreadView{}, false
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

// threadCollapsed reports whether a thread shows as one line.
func (m *model) threadCollapsed(t review.ThreadView) bool {
	if collapsed, ok := m.threadFolds[t.ID]; ok {
		return collapsed
	}
	return t.State == store.ThreadResolved
}

func (m *model) setThreadState(t review.ThreadView, state store.ThreadState) tea.Cmd {
	// Back to the default, so resolving collapses a thread and reopening expands it.
	delete(m.threadFolds, t.ID)
	svc, ctx := m.svc, m.ctx
	return func() tea.Msg {
		return opMsg{status: fmt.Sprintf("thread %s %s", t.Label(), state), err: svc.SetThreadState(ctx, t.ID, state)}
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
	if cur.gap != nil {
		return nt, "", errors.New("unchanged lines outside the diff cannot be commented on yet")
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
		c.title = fmt.Sprintf("Reply to %s", t.Label())
	case composeEdit:
		own, ok := m.lastOwnComment(t)
		// In the overview, edit the selected comment when it is yours.
		if seg := m.main.itemSegment(); m.main.reader && seg < len(t.Comments) && t.Comments[seg].Author == m.svc.Author() {
			own, ok = t.Comments[seg], true
		}
		if !ok {
			m.setError(errors.New("no comment of yours in this thread"))
			return m, nil
		}
		c.title, c.commentID, initial = fmt.Sprintf("Edit your comment on %s", t.Label()), own.ID, own.Body
		if t.State == store.ThreadResolved {
			// Edits after resolving are easy to miss: nobody rereads a resolved thread.
			c.title += " (resolved: the edit will not reopen it)"
			c.warn = true
		}
	}
	return m, m.openComposer(c, initial)
}

func (m *model) openComposer(c composer, initial string) tea.Cmd {
	c.area = textarea.New()
	c.area.ShowLineNumbers = false
	c.area.Placeholder = "Markdown. ctrl+s to save, ctrl+e for $EDITOR, esc to cancel."
	c.area.SetStyles(m.composeStyles())
	c.area.SetValue(initial)
	m.compose = c
	m.mode = modeCompose
	m.relayout(false)
	m.main.ensureVisible()
	return m.compose.area.Focus()
}

// composeStyles colors the comment editor from the palette, so text and hints are readable on any
// background.
func (m *model) composeStyles() textarea.Styles {
	st := m.r.st
	styles := textarea.DefaultStyles(st.isDark)
	for _, state := range []*textarea.StyleState{&styles.Focused, &styles.Blurred} {
		state.Text = lipgloss.NewStyle().Foreground(st.fg)
		state.Placeholder = st.muted
		state.CursorLine = lipgloss.NewStyle().Foreground(st.fg)
		state.EndOfBuffer = st.muted
		state.Prompt = st.muted
	}
	return styles
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

// verdictKey takes the verdict: a, r, or c open the summary editor, and A, R, or C submit straight
// away with no new summary, keeping any already in the draft.
func (m *model) verdictKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	verdicts := map[string]store.Verdict{"a": store.VerdictApprove, "r": store.VerdictRequestChanges, "c": store.VerdictComment}
	key := msg.String()
	verdict, ok := verdicts[strings.ToLower(key)]
	if !ok || len(key) != 1 {
		m.mode = modeNormal
		return m, nil
	}
	if key != strings.ToLower(key) {
		m.mode = modeNormal
		svc, ctx, change := m.svc, m.ctx, m.data.change
		return m, func() tea.Msg {
			r, err := svc.Submit(ctx, change, verdict, "")
			return opMsg{status: fmt.Sprintf("submitted review #%d: %s", r.Seq, r.Verdict), err: err}
		}
	}
	initial := ""
	if d, ok := m.data.draft(); ok {
		initial = d.Body
	}
	return m, m.openComposer(composer{kind: composeSubmit, verdict: verdict, title: "Submit review: " + string(verdict) + " (summary is optional)"}, initial)
}

// confirmation is a destructive action waiting for y.
type confirmation struct {
	prompt string
	run    func() tea.Msg
}

// ask puts a destructive action behind a y/n prompt.
func (m *model) ask(prompt string, run func() tea.Msg) {
	m.confirm = confirmation{prompt: prompt, run: run}
	m.mode = modeConfirm
}

func (m *model) confirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	c := m.confirm
	m.confirm = confirmation{}
	if msg.String() != "y" || c.run == nil {
		return m, nil
	}
	return m, c.run
}

// deleteReviewAtCursor deletes the review whose entry the cursor is on, or discards the draft
// when the cursor is elsewhere, after asking.
func (m *model) deleteReviewAtCursor() tea.Cmd {
	svc, ctx, change := m.svc, m.ctx, m.data.change
	p := &m.main
	if p.cursor < len(p.rows) && p.rows[p.cursor].kind == rowReview && p.rows[p.cursor].review.State == store.ReviewSubmitted {
		rv := *p.rows[p.cursor].review
		if rv.Author != svc.Author() {
			m.setError(fmt.Errorf("review #%d is by %s", rv.Seq, rv.Author.Name))
			return nil
		}
		m.ask(fmt.Sprintf("Delete your review #%d with its summary and threads? y/n", rv.Seq), func() tea.Msg {
			return opMsg{status: fmt.Sprintf("review #%d deleted", rv.Seq), err: svc.DeleteReview(ctx, change, rv.Seq)}
		})
		return nil
	}
	if _, ok := m.data.draft(); !ok {
		m.setError(errors.New("no draft review; on a review of yours, D deletes it"))
		return nil
	}
	m.ask("Discard your draft review and all its comments? y/n", func() tea.Msg {
		return opMsg{status: "draft review discarded", err: svc.Discard(ctx, change)}
	})
	return nil
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
	if m.main.reader {
		m.main.readerWheel(delta)
	} else {
		m.main.scroll(delta)
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
	if nw > 0 && msg.X == nw {
		// The divider: drag to resize the sidebar.
		m.dragging = true
		return m, nil
	}
	if vh, fh := m.sidebarHeights(bh); msg.X < nw && fh > 0 && msg.Y == vh {
		// The divider between the commit list and the file list: drag to resize them.
		m.draggingSplit = true
		return m, nil
	}
	if msg.X < nw {
		l, f, i := m.sidebarAt(msg.Y)
		vh, fh := m.sidebarHeights(bh)
		rows, y := vh-1, msg.Y-1
		if f == focusFiles {
			rows, y = fh-1, msg.Y-vh-sidebarGap-1
		}
		if dir := l.indicatorAt(y, rows); dir != 0 {
			// An indicator of more entries: move the selection a page that way.
			m.setFocus(f)
			l.move(dir * max(rows-2, 1))
			if f == focusViews {
				m.openView()
			} else {
				m.openFile()
			}
			return m, nil
		}
		m.setFocus(f)
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
	m.setFocus(focusMain)
	i := m.main.rowAt(msg.Y - 1)
	if i < 0 || !m.main.rows[i].selectable() {
		return m, nil
	}
	m.main.cursor = i
	if m.main.reader {
		m.main.selectAt(m.main.offset + msg.Y - 1)
	}
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
	if m.draggingSplit {
		m.viewsHeight = max(msg.Y, 2)
		return m, nil
	}
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
		return m.renderHelp()
	}
	if m.data == nil {
		msg := "loading..."
		if m.statusErr {
			msg = m.status
		}
		return msg
	}
	nw, mw, bh := m.layoutSizes()
	body := m.main.visible(m.renderMainRow)
	if m.search != "" {
		for i := range body {
			body[i] = m.highlightMatches(body[i])
		}
	}
	mainLines := append([]string{m.mainHeader(mw)}, body...)
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
		hints := "  " + m.help.ShortHelpView([]key.Binding{m.keys.Save, m.keys.ExternalEdit, m.keys.Cancel})
		titleStyle := m.r.st.heading
		if m.compose.warn {
			titleStyle = m.r.st.badgeWarn.Bold(true)
		}
		title := titleStyle.Render(m.compose.title) + hints
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
	case modeSearch:
		return m.searchInput.View()
	case modeVerdict:
		key := func(k, desc string) string { return st.helpKey.Render(k) + st.helpDesc.Render(desc) }
		return st.heading.Render("Submit: ") + key("a", "pprove  ") + key("r", "equest changes  ") + key("c", "omment") +
			st.muted.Render(" with a summary; ") + key("A R C", " without one; ") + st.muted.Render("other keys cancel")
	case modeConfirm:
		return st.statusErr.Render(m.confirm.prompt)
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
	if m.autoAdvance {
		left += st.badgeWarn.Render(" auto-advance")
	}
	if m.split {
		left += " split"
	}
	if m.main.anchor >= 0 {
		left += st.badgeWarn.Render(" selecting")
	}
	if label := m.searchLabel(); label != "" {
		left += " " + st.searchMatch.Render(label) + " " + st.helpKey.Render("esc") + st.helpDesc.Render(" clears")
	}
	// Fit the hints in what the left side leaves, keeping help and quit.
	right := m.renderHints(m.shortHelp(), max(m.width-lipgloss.Width(left)-2, 10))
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right)-1, 1)
	return fit(st.statusBar.Render(left)+strings.Repeat(" ", gap)+right, m.width, nil)
}
