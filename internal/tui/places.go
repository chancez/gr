package tui

import (
	"context"
	"encoding/json/v2"
	"log/slog"
	"maps"

	"github.com/chancez/gr/internal/store"
)

// place is where you were in a change: the view and the row the cursor is on, how far down the
// screen it was, and how the change was folded and split. It is saved when gr quits, so opening the
// change again goes back there.
type place struct {
	View        string          `json:"view"`
	Row         string          `json:"row"`
	Segment     int             `json:"segment,omitzero"`
	Y           int             `json:"y,omitzero"`
	Split       bool            `json:"split,omitzero"`
	Focus       focus           `json:"focus,omitzero"`
	Expanded    map[string]bool `json:"expanded,omitempty"`
	OpenGaps    map[string]bool `json:"open_gaps,omitempty"`
	ThreadFolds map[string]bool `json:"thread_folds,omitempty"`
}

// readPlace reads where you were in a change. None, or one gr cannot read, is no place: opening
// where gr would anyway is better than not opening.
func (m *model) readPlace(change store.Change) (place, bool) {
	b, ok, err := m.svc.Place(m.ctx, change)
	var p place
	if err == nil && ok {
		err = json.Unmarshal(b, &p)
	}
	if err != nil {
		slog.Warn("cannot read the saved place", "change", change.Seq, "err", err)
		return place{}, false
	}
	return p, ok
}

// placeHere is where the model is.
func (m *model) placeHere() place {
	at := m.here()
	return place{
		View:        at.view,
		Row:         at.row,
		Segment:     at.seg,
		Y:           at.y,
		Split:       m.split,
		Focus:       m.focus,
		Expanded:    maps.Clone(m.expanded),
		OpenGaps:    maps.Clone(m.openGaps),
		ThreadFolds: maps.Clone(m.threadFolds),
	}
}

// goTo opens a saved place: its folds and split first, as they decide the rows, then its view and
// row, once the view's diff has loaded. What is gone since, as a rewritten commit, is left where
// gr opens anyway.
func (m *model) goTo(p place) {
	maps.Copy(m.expanded, p.Expanded)
	maps.Copy(m.openGaps, p.OpenGaps)
	maps.Copy(m.threadFolds, p.ThreadFolds)
	m.split = p.Split
	m.rebuildMain()
	m.restore(jumpPoint{view: p.View, row: p.Row, seg: p.Segment, y: p.Y})
	if m.needsLoad(m.views[m.current]) {
		m.pendingPlace = &jumpPoint{view: p.View, row: p.Row, seg: p.Segment, y: p.Y}
	}
	if p.Focus != focusMain || m.focus != focusMain {
		m.setFocus(p.Focus)
	}
}

// savePlace saves where the model is in its change. It runs as gr quits, so it does not wait on
// the context gr ran in.
func (m *model) savePlace() {
	if m.data == nil {
		return
	}
	b, err := json.Marshal(m.placeHere())
	if err == nil {
		err = m.svc.SavePlace(context.WithoutCancel(m.ctx), m.data.change, b)
	}
	if err != nil {
		slog.Warn("cannot save the place", "err", err)
	}
}
