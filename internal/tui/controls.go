package tui

import (
	"github.com/chancez/gr/internal/tui/interact"
	"github.com/chancez/gr/internal/tui/look"
)

// A control is an action drawn as a button, which a click runs. It is found on the target its
// key and its menu run it on, so a control does what its key does there, shows that key, and is
// drawn only while the action applies.

// control is an action to draw as a control, and how it looks.
type control struct {
	action any
	label  string
	look   controlLook
	// on is whether a checkbox is checked, or a chevron open.
	on bool
	// lit shows a quiet control as the mouse or cursor reaches it.
	lit bool
}

// controlLook is how a control is drawn.
type controlLook int

const (
	lookButton controlLook = iota
	lookCheckbox
	lookIcon
	// The quiet looks, for controls inside content.
	lookLink
	lookQuietCheckbox
	lookChevron
)

// button is a control for a, a button labeled label.
func button[S any](a *interact.Action[*model, S], label string) control {
	return control{action: a, label: label, look: lookButton}
}

// checkbox is a control for a, a box labeled label, for an action that toggles what the box shows.
func checkbox[S any](a *interact.Action[*model, S], label string) control {
	return control{action: a, label: label, look: lookCheckbox}
}

// icon is a control for a drawn as one character, glyph, without its key.
func icon[S any](a *interact.Action[*model, S], glyph string) control {
	return control{action: a, label: glyph, look: lookIcon}
}

// link is a quiet control for a, labeled label.
func link[S any](a *interact.Action[*model, S], label string) control {
	return control{action: a, label: label, look: lookLink}
}

// quietCheckbox is a quiet checkbox for a, labeled label.
func quietCheckbox[S any](a *interact.Action[*model, S], label string) control {
	return control{action: a, label: label, look: lookQuietCheckbox}
}

// chevron is a quiet control for a that folds and unfolds.
func chevron[S any](a *interact.Action[*model, S]) control {
	return control{action: a, look: lookChevron}
}

// lighted is c, lit when on.
func (c control) lighted(on bool) control {
	c.lit = on
	return c
}

// labeled is c, labeled label, for a control whose label says what it shows, as a filter's.
func (c control) labeled(label string) control {
	c.label = label
	return c
}

// checked is c, checked when on.
func (c control) checked(on bool) control {
	c.on = on
	return c
}

// spans draws c, with key, the key that runs it.
func (c control) spans(key string) []look.Span {
	switch c.look {
	case lookCheckbox:
		return look.Checkbox(c.label, key, c.on)
	case lookIcon:
		return []look.Span{look.Icon(c.label)}
	case lookLink:
		return look.Link(c.label, key, c.lit)
	case lookQuietCheckbox:
		return look.QuietCheckbox(c.label, key, c.on, c.lit)
	case lookChevron:
		return []look.Span{look.Chevron(c.on, c.lit)}
	}
	return look.Button(c.label, key)
}

// The controls of rows and the title bar. They are made in init, as kinds are: their actions draw
// rows.
var (
	viewedBox       control
	commitViewedBox control
	fileFold        control
	expandFold      control
	commentHere     control
	replyButton     control
	resolveButton   control
	reopenButton    control
	addressedButton control
	editButton      control
	deleteButton    control
	deleteReview    control
	prevCommit      control
	nextCommit      control
	filterButton    control
)

func init() {
	// Inside the diff and the overview, controls are quiet; the + under the mouse is the exception.
	viewedBox = quietCheckbox(actMarkFileReviewed, "Viewed")
	commitViewedBox = quietCheckbox(actMarkCommitReviewed, "Viewed")
	fileFold = chevron(actFoldFile)
	expandFold = link(actExpandFold, "")
	commentHere = icon(actCommentOnLine, "+")
	replyButton = link(actReplyToThread, "Reply")
	resolveButton = link(actResolveThread, "Resolve")
	reopenButton = link(actResolveThread, "Reopen")
	addressedButton = link(actAddressThread, "Addressed")
	editButton = link(actEditComment, "Edit")
	deleteButton = link(actDeleteComment, "Delete")
	deleteReview = link(actDeleteReview, "Delete")
	prevCommit = button(actPrevCommit, "Prev")
	nextCommit = button(actNextCommit, "Next")
	filterButton = button(actFilterThreads, "")
}

const controlZone = "control:"

// drawnControl is a control on screen: what it runs, and the mode it runs in.
type drawnControl struct {
	run  interact.Bound[*model]
	mode mode
}

// controls draws the controls of cs that t offers and that apply, a space apart, to run in mode
// md. where names where they are drawn, unique on screen, such as a row.
func (m *model) controls(where string, md mode, t interact.Target[*model], cs ...control) []look.Span {
	keys := interact.Keys(m, t)
	var out []look.Span
	for _, c := range cs {
		b, key, ok := offered(m, t, keys, c.action)
		if !ok {
			continue
		}
		if len(out) > 0 {
			gap := " "
			if c.look >= lookLink {
				// Quiet controls have no chip to set them apart.
				gap = "  "
			}
			out = append(out, look.Default(gap))
		}
		out = append(out, m.mark(where, md, b, c.spans(key)...)...)
	}
	return out
}

// mark marks spans as a control that runs b in mode md, and records it for a click on where it
// lands. Its zone id names where it is and its action, never its turn in a frame, so a row's
// drawing kept from an earlier frame is still the control it was.
func (m *model) mark(where string, md mode, b interact.Bound[*model], spans ...look.Span) []look.Span {
	id := controlZone + where + ":" + b.ID()
	if m.drawnControls == nil {
		m.drawnControls = map[string]drawnControl{}
	}
	m.drawnControls[id] = drawnControl{run: b, mode: md}
	return look.Mark(id, spans...)
}

// offered is action a bound on t, and the first key that runs it there, if any, while it applies.
func offered(m *model, t interact.Target[*model], keys []interact.Key[*model], a any) (interact.Bound[*model], string, bool) {
	for _, k := range keys {
		if k.Action.Is(a) {
			// A button shows one key: its help can cover two bindings, as "}/{" does.
			return k.Action, k.Keys()[0], true
		}
	}
	for _, g := range t.Groups(m) {
		for _, b := range g.Actions {
			if b.Is(a) {
				return b, "", true
			}
		}
	}
	return interact.Bound[*model]{}, "", false
}

// controlAt is what the control drawn at x, y in the last frame runs, if one is there and runs in
// the mode open now: a row's controls do nothing behind the comment box.
func (m *model) controlAt(x, y int) (interact.Bound[*model], bool) {
	for id, zones := range m.zones {
		c, ok := m.drawnControls[id]
		if !ok || c.mode != m.mode {
			continue
		}
		for _, z := range zones {
			if z.Contains(x, y) {
				return c.run, true
			}
		}
	}
	return interact.Bound[*model]{}, false
}
