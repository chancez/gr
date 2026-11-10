// Package tui is gr's terminal interface, where reviews are written.
package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/termtext"
	"github.com/chancez/gr/internal/tui/look"
)

// The built-in themes, one of which Options picks.
const (
	ThemeDefault    = look.ThemeDefault
	ThemeColorblind = look.ThemeColorblind
)

// Themes lists the built-in themes.
var Themes = look.Themes

// Options configure the TUI.
type Options struct {
	// Theme is a built-in palette, one of Themes. Empty is ThemeDefault.
	Theme string
	// Notice is shown in the status bar when the TUI opens, such as a base gr chose.
	Notice string
	// Rename is a change of the branch the one opened was copied from, which the TUI offers to
	// move onto it.
	Rename *review.Rename
	// Glyphs is "ascii", "unicode", or "nerd". Empty picks them from what the terminal is.
	Glyphs string
	// Scrolloff is the lines kept between the cursor and the main pane's edges. Nil is the default.
	Scrolloff *int
	// SidebarWidth is the sidebar's width until it is dragged. Zero sizes it to the terminal.
	SidebarWidth int
	// AutoAdvance starts with moving on to the next unreviewed file after marking one reviewed.
	AutoAdvance bool
	// Vim edits comments with vim's modes.
	Vim bool
	// OpenRepo opens a service for the repository at dir, for its changes to open in tabs. Nil
	// keeps tabs to the first change's repository.
	OpenRepo func(ctx context.Context, dir string) (*review.Service, error)
}

// Run opens the TUI on a change.
func Run(ctx context.Context, svc *review.Service, change store.Change, opts Options) error {
	if opts.Theme != "" && !slices.Contains(Themes, opts.Theme) {
		return fmt.Errorf("unknown theme %q, want one of %s", opts.Theme, strings.Join(Themes, ", "))
	}
	if opts.Glyphs != "" {
		if _, ok := look.ParseGlyphs(opts.Glyphs); !ok {
			return fmt.Errorf("unknown glyphs %q, want ascii, unicode, or nerd", opts.Glyphs)
		}
	}
	m, err := newTab(ctx, svc, change, opts)
	if err != nil {
		return err
	}
	if opts.Notice != "" {
		m.setStatus("%s", opts.Notice)
	}
	a := newApp(ctx, opts, m)
	defer a.quit()
	_, err = tea.NewProgram(a, tea.WithContext(ctx)).Run()
	return err
}

// newTab opens change c of svc's repository with gr's settings, in a model of its own.
func newTab(ctx context.Context, svc *review.Service, change store.Change, opts Options) (*model, error) {
	m, err := newModel(ctx, svc, change)
	if err != nil {
		return nil, err
	}
	m.theme = opts.Theme
	if g, ok := look.ParseGlyphs(opts.Glyphs); ok {
		m.glyphs, m.glyphsSet = g, true
	} else {
		m.glyphs = look.GlyphsFromEnv()
	}
	m.setTheme(true)
	if opts.Scrolloff != nil {
		m.main.scrolloff = *opts.Scrolloff
	}
	m.navWidth, m.autoAdvance, m.vim = opts.SidebarWidth, opts.AutoAdvance, opts.Vim
	m.openRepo, m.opened = opts.OpenRepo, change
	if p, ok := m.readPlace(change); ok {
		m.savedPlace = &p
	}
	if opts.Rename != nil && opts.Rename.Kept {
		m.offerRename(*opts.Rename)
	}
	return m, nil
}

// shownChange is the change the model shows: as loaded, or as it was opened until then.
func (m *model) shownChange() store.Change {
	if m.data == nil {
		return m.opened
	}
	return m.data.change
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
	modePicker
)

type model struct {
	ctx      context.Context
	svc      *review.Service
	changeID string
	help     help.Model
	r        *renderer

	data    *changeData
	views   []view
	current int
	// viewsNav lists the overview, interdiff, commits, and whole diff. filesNav lists the current
	// view's files.
	viewsNav navList
	filesNav navList
	// navWidth is the sidebar width once dragged or set in the config, or 0 for the default.
	navWidth int
	dragging bool
	// search is the last search pattern, and searchInput the prompt while typing one. searchPos
	// and searchHits are the current match's position among the matches.
	search      string
	searchPos   int
	searchHits  int
	searchInput textinput.Model
	// picker is the popup open in modePicker.
	picker *picker
	// vim edits comments with vim's modes.
	vim bool
	// repoNames are the names of repositories by directory, as git gives them.
	repoNames map[string]string
	// opened is the change the model was opened on, which names its tab until its data loads.
	opened store.Change
	// openRepo opens another repository, for its changes to open in tabs.
	openRepo func(ctx context.Context, dir string) (*review.Service, error)
	// asked is what askAboutLanding asked already, so it is asked once.
	asked map[string]bool
	// renamed is, while a branch picked is opened, the change of the branch it was copied from,
	// for whichever tab shows it to offer.
	renamed *review.Rename
	// savedPlace is where you were in the change, to open once it loads, and pendingPlace where to
	// go once its view's diff does.
	savedPlace   *place
	pendingPlace *jumpPoint
	// glyphs are the symbols drawn with, and glyphsSet is set when they were asked for rather than
	// picked from what the terminal is.
	glyphs    look.Glyphs
	glyphsSet bool
	// zones are where the last frame drew marked spans, such as mentions, by id.
	zones map[string][]look.Zone
	// drawnControls are the controls drawn, by zone id. They are kept as long as the rows, whose
	// drawings are kept across frames.
	drawnControls map[string]drawnControl
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
	// selecting is set while the mouse drags a line selection from the + that comments.
	selecting *selecting
	// pendingLoads are views whose diffs to load once the current message is handled, and
	// syncLoads loads them at once instead, for tests. pendingThread and pendingFile are where to go
	// once the current view's diff arrives, pendingThread in the whole diff when pendingInDiff.
	pendingLoads  []int
	syncLoads     bool
	pendingThread *review.ThreadView
	pendingInDiff bool
	pendingFile   string
	// comparing is the revisions picked to compare in the interdiff, by seq, if any.
	comparing *[2]int
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
	// revised is set when the action recorded a new revision, so git needs reading again.
	revised bool
}

type tickMsg struct{}

// refreshMsg reports another process's writes (changed), or the branch or its base moving
// (headMoved), which needs git read again.
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
		ctx:      ctx,
		svc:      svc,
		changeID: change.ID,
		watcher:  w,
		version:  version,
		main: pane{
			anchor:    -1,
			side:      anchor.SideNew,
			scrolloff: defaultScrolloff,
		},
		expanded:    map[string]bool{},
		openGaps:    map[string]bool{},
		threadFolds: map[string]bool{},
		contents:    map[string][]string{},
		returns:     map[viewKind]jumpPoint{},
		lastSidebar: focusFiles,
	}
	// Dark until the terminal reports its background.
	m.setTheme(true)
	return m, nil
}

// setTheme rebuilds colors for a dark or light background.
func (m *model) setTheme(isDark bool) {
	m.isDark = isDark
	m.r = newRenderer(isDark, m.theme, m.glyphs)
	h := help.New()
	h.ShortSeparator, h.FullSeparator, h.Ellipsis = " | ", "   ", "..."
	m.r.th.StyleHelp(&h)
	h.SetWidth(m.width)
	m.help = h
}

func (m *model) close() {
	if m.watcher != nil {
		m.watcher.Close()
	}
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadCmd(), tick(), tea.RequestBackgroundColor}
	if !m.glyphsSet {
		cmds = append(cmds, tea.RequestTerminalVersion)
	}
	return tea.Batch(cmds...)
}

// How long messages stay in the status bar before the key hints come back.
const (
	statusTTL = 5 * time.Second
	errorTTL  = 10 * time.Second
)

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
		// Rows drawn so far hold the old theme's renderer.
		m.rebuildMain()
		m.relayout(true)
		return m, nil
	case tea.TerminalVersionMsg:
		// The terminal's name only adds Nerd Font icons, for a terminal that bundles them: a
		// multiplexer answers as itself, though the terminal outside it may bring them.
		if !m.glyphsSet && look.GlyphsFor(msg.Name) == look.GlyphsNerd && m.glyphs != look.GlyphsNerd {
			m.glyphs = look.GlyphsNerd
			m.setTheme(m.isDark)
			m.rebuildMain()
			m.relayout(true)
		}
		return m, nil
	case dataMsg:
		if msg.err == nil && msg.data.change.ID != m.changeID {
			// Loaded for the change shown before a switch.
			return m, nil
		}
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		m.applyData(msg.data)
		return m, nil
	case viewLoadedMsg:
		m.applyView(msg)
		return m, nil
	case landedMsg:
		// Only into the picker it was looked up for, while it is open.
		if m.mode == modePicker && m.picker == msg.picker && len(msg.landed) > 0 {
			m.picker.drop(msg.landed)
		}
		return m, nil
	case openedMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		cmd := m.switchChange(msg.change)
		m.setStatus("%s", msg.status)
		return m, cmd
	case movedMsg:
		if msg.err != nil {
			m.setError(msg.err)
			return m, nil
		}
		cmd := m.switchChange(msg.change)
		m.setStatus("change #%d follows %s now", msg.change.Seq, msg.change.Branch)
		return m, cmd
	case opMsg:
		if msg.err != nil {
			m.setError(msg.err)
		} else if msg.status != "" {
			m.setStatus("%s", msg.status)
		}
		if msg.revised {
			return m, m.loadCmd()
		}
		// An action changes the database, not git: reload just that.
		return m, m.reloadCmd()
	case tickMsg:
		m.expireStatus(time.Now())
		return m, tea.Batch(m.refreshCmd(), tick())
	case refreshMsg:
		m.version = msg.version
		// A question about the change that waited on something else open, as each tick looks.
		m.askAboutLanding()
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
		if m.selecting != nil {
			return m, m.selected()
		}
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
		case modePicker:
			return m.pickerKey(msg)
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
	return navWidth, max(mainWidth, 10), max(bodyHeight, 1)
}

// composeHeight is the comment box's text area, in lines.
const composeHeight = 6

// mainHeight is the main pane's row area, below its header line.
func mainHeight(bodyHeight int) int { return max(bodyHeight-1, 1) }

// pinned are the lines pinned under the main pane's title bar, which stay put while it scrolls: the
// overview's change details.
func (m *model) pinned() [][]look.Span {
	if m.data == nil || m.current >= len(m.views) || m.views[m.current].kind != viewOverview {
		return nil
	}
	details := overviewDetails(m.data)
	if line := m.newWorkLine(); line != nil {
		// Before the blank line that ends them.
		details = append(details[:len(details)-1:len(details)-1], line, details[len(details)-1])
	}
	return details
}

// mainTop is the screen line the main pane's rows start on, under its title bar and what is
// pinned there.
func (m *model) mainTop() int { return 1 + len(m.pinned()) }

// rowsHeight is the main pane's height for rows, less what is pinned.
func (m *model) rowsHeight(bodyHeight int) int { return max(mainHeight(bodyHeight)-len(m.pinned()), 1) }

// relayout recomputes row heights after a size change.
func (m *model) relayout(resized bool) {
	_, mw, bh := m.layoutSizes()
	m.main.height = m.rowsHeight(bh)
	if resized || m.main.width != mw {
		m.main.width = mw
		if m.data != nil {
			m.main.layout(m.renderMainRow)
		}
	}
	m.main.clamp()
}

func (m *model) normalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.pending != "" {
		return m.pendingKey(msg)
	}
	cmd, _ := m.dispatch(msg)
	return m, cmd
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

// confirmation is a destructive action waiting for y, or a choice between two, waiting for y or n.
type confirmation struct {
	prompt string
	run    func() tea.Msg
	// no runs on n, for a choice whose no is an answer of its own, as abandoned is to merged, and
	// yes and noLabel name the answers. Without it, n is no, as esc and any other key are.
	no      func() tea.Msg
	yes     string
	noLabel string
}

// ask puts a destructive action behind a y/n prompt.
func (m *model) ask(prompt string, run func() tea.Msg) {
	m.confirm = confirmation{prompt: prompt, run: run}
	m.mode = modeConfirm
}

// choose asks to pick between two answers, yes on y and no on n, or neither for now on esc.
func (m *model) choose(prompt, yesLabel string, yes func() tea.Msg, noLabel string, no func() tea.Msg) {
	m.confirm = confirmation{prompt: prompt, run: yes, no: no, yes: yesLabel, noLabel: noLabel}
	m.mode = modeConfirm
}

// confirmKey answers the y/n prompt: any key but its own is no.
func (m *model) confirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	c, ok := m.runOn(msg, theConfirm.Target(confirmSubject{}))
	if !ok {
		m.endConfirm()
	}
	return m, c
}

// endConfirm closes the y/n prompt.
func (m *model) endConfirm() {
	m.mode = modeNormal
	m.confirm = confirmation{}
}

func (m *model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	// Every motion, not only drags, so what the mouse is over can show its controls.
	v.MouseMode = tea.MouseModeAllMotion
	v.Cursor = m.composeCursor()
	v.WindowTitle = "gr"
	if m.data != nil {
		v.WindowTitle = fmt.Sprintf("gr #%d %s", m.data.change.Seq, termtext.Line(m.data.change.Title))
	}
	return v
}

// render draws the frame, and records where its marked spans landed for the mouse.
func (m *model) render() string {
	out, zones := look.Scan(m.frame())
	m.zones = zones
	return out
}

func (m *model) frame() string {
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
	m.main.focusThread = m.focusedThread()
	body := m.main.visible(m.renderMainRow)
	if h, ok := m.stickyFile(); ok && len(body) > 0 {
		body[0] = m.renderMainRow(h, rowState{cursor: h == m.main.cursor})[0]
	}
	if m.search != "" {
		for i := range body {
			body[i] = m.highlightMatches(body[i])
		}
	}
	mainLines := []string{m.mainHeader(mw)}
	for _, l := range m.pinned() {
		// Past the margin the overview's reader marks its item in, as its rows are.
		mainLines = append(mainLines, m.r.th.Draw(look.Plain, mw, append([]look.Span{look.Default(" ")}, l...)...))
	}
	mainLines = append(mainLines, body...)
	navLines := m.renderSidebar(nw, bh)
	sep := m.r.th.Render(look.Plain, look.Border("|", m.dragging))
	screen := make([]string, bh)
	for y := range bh {
		var line strings.Builder
		if nw > 0 {
			nl := ""
			if y < len(navLines) {
				nl = navLines[y]
			}
			line.WriteString(look.Fit(nl, nw))
			line.WriteString(sep)
		}
		ml := ""
		if y < len(mainLines) {
			ml = mainLines[y]
		}
		line.WriteString(look.Fit(ml, mw))
		screen[y] = line.String()
	}
	if m.mode == modePicker {
		m.picker.layout(m.width, bh)
		screen = overlay(screen, m.picker.render(m.r.th), m.picker.x, m.picker.y)
	}
	var b strings.Builder
	for _, line := range screen {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(m.renderStatus())
	return b.String()
}

// clearTransient clears the selection, the status message, and the search.
func (m *model) clearTransient() {
	m.main.anchor = -1
	m.status = ""
	m.search, m.searchPos, m.searchHits = "", 0, 0
}

func (m *model) toggleAutoAdvance() {
	m.autoAdvance = !m.autoAdvance
	if m.autoAdvance {
		m.setStatus("auto-advance on: marking reviewed moves to the next unreviewed file")
	} else {
		m.setStatus("auto-advance off: marking reviewed stays put")
	}
}
