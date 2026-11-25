package tui

import (
	"context"
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// polled marks the load cmd makes as a refresh's.
func polled(cmd tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		msg := cmd().(dataMsg)
		msg.polled = true
		return msg
	}
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
	if m.data == nil || msg.head != m.data.revision.HeadSHA || msg.tree != m.data.uncommitted.Tree || msg.opts != m.data.opts {
		return
	}
	m.closeStaleMenus()
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
			m.openThread(*t, m.pendingInDiff)
		}
		if p := m.pendingPlace; p != nil {
			m.pendingPlace = nil
			m.restore(*p)
		}
	}
}

func (m *model) loadCmd() tea.Cmd {
	ctx, svc, id, old, opts := m.ctx, m.svc, m.changeID, m.data, m.diffOptions()
	current := ""
	if m.data != nil && m.current < len(m.views) {
		current = m.views[m.current].key()
	}
	return func() tea.Msg {
		start := time.Now()
		d, err := load(ctx, svc, id, opts)
		if err == nil {
			// Keep what is still valid, and load the view on screen, before showing anything.
			d.carryLoaded(old)
			err = d.preload(ctx, svc, current)
		}
		slog.Debug("reload", "duration", time.Since(start), "err", err)
		return dataMsg{data: d, err: err}
	}
}

// refreshCmd checks whether another process wrote to the database and, with withGit, whether the
// branch moved or what is uncommitted in its worktree changed.
func (m *model) refreshCmd(withGit bool) tea.Cmd {
	ctx, svc, w, last := m.ctx, m.svc, m.watcher, m.version
	var change store.Change
	var tips review.Tips
	var uncommitted review.Uncommitted
	if m.data != nil {
		change, tips, uncommitted = m.data.change, m.data.tips, m.data.uncommitted
	}
	// Shown, a file changed again is seen, which git's status does not tell.
	watching := m.current < len(m.views) && (m.views[m.current].kind == viewUncommitted || m.views[m.current].uncommitted)
	return func() tea.Msg {
		v, err := w.Version(ctx)
		if err != nil {
			return refreshMsg{version: last}
		}
		msg := refreshMsg{version: v, changed: v != last}
		if withGit && change.Kind == store.KindBranch {
			// Closed changes too: new work on a merged change's branch is a new change's to offer.
			// The base moving can merge an open change, as fast-forwarding main onto the branch does.
			if now := svc.Tips(ctx, change); now.Head != "" && now != tips {
				msg.headMoved = true
			}
			msg.uncommittedMoved = uncommittedMoved(ctx, svc, change, uncommitted, watching)
		}
		return msg
	}
}

// applyData swaps in freshly loaded data, keeping the view and cursor where they were.
func (m *model) applyData(d *changeData) {
	first := m.data == nil
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
	m.closeStaleMenus()
	if !first && d.opts != m.data.opts {
		// The options changed while it loaded: what it loaded is shown until loaded again with them.
		d.opts, d.loaded = m.data.opts, map[string]bool{}
	}
	if !first && m.data.change.State == store.ChangeOpen && d.change.State == store.ChangeMerged {
		m.setStatus("change #%d merged into %s", d.change.Seq, d.change.BaseBranch)
	}
	m.data = d
	if c := m.comparing; c != nil && !d.compare(c[0], c[1]) {
		m.comparing = nil
	}
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
	switch p := m.savedPlace; {
	case p != nil:
		m.savedPlace = nil
		m.goTo(*p)
	case first && m.views[m.current].kind == viewOverview:
		// With nowhere saved, the conversation opens at its newest end, as a chat does.
		m.main.readerSelect(len(m.main.items) - 1)
	}
	m.askAboutLanding()
}

// uncommittedMoved reports whether what change has uncommitted is not old any more: its status
// changed, or, when watching, its files.
func uncommittedMoved(ctx context.Context, svc *review.Service, change store.Change, old review.Uncommitted, watching bool) bool {
	if !watching {
		status, err := svc.UncommittedStatus(ctx, change)
		return err == nil && status != old.Status
	}
	u, err := svc.Uncommitted(ctx, change)
	return err == nil && (u.Status != old.Status || u.Tree != old.Tree)
}
