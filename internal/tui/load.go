package tui

import (
	"log/slog"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/chancez/gr/internal/store"
)

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
	m.closeStaleMenus()
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
}
