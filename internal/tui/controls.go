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
	icon   look.Glyph
	label  string
	look   controlLook
	// on is whether a checkbox is checked, or a chevron open.
	on bool
	// lit shows a quiet control as the mouse or cursor reaches it.
	lit bool
	// after is drawn after a chevron, as part of it, so a click on what it heads, as a file's name,
	// folds too: a chevron alone is one cell to hit.
	after []look.Span
	// off draws the control greyed, for no click to run, where there is nothing for it to do.
	off bool
	// selects makes a press on the control start a line selection, and the release run it on what
	// was selected, as dragging GitHub's + comments on several lines.
	selects bool
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

// button is a control for a, a button with an icon and a label.
func button[S any](a *interact.Action[*model, S], icon look.Glyph, label string) control {
	return control{action: a, icon: icon, label: label, look: lookButton}
}

// checkbox is a control for a, a box labeled label, for an action that toggles what the box shows.
func checkbox[S any](a *interact.Action[*model, S], label string) control {
	return control{action: a, label: label, look: lookCheckbox}
}

// icon is a control for a drawn as one symbol, without a label or its key.
func icon[S any](a *interact.Action[*model, S], g look.Glyph) control {
	return control{action: a, icon: g, look: lookIcon}
}

// link is a quiet control for a, with an icon and a label.
func link[S any](a *interact.Action[*model, S], icon look.Glyph, label string) control {
	return control{action: a, icon: icon, label: label, look: lookLink}
}

// quietCheckbox is a quiet checkbox for a, labeled label.
func quietCheckbox[S any](a *interact.Action[*model, S], label string) control {
	return control{action: a, label: label, look: lookQuietCheckbox}
}

// chevron is a quiet control for a that folds and unfolds.
func chevron[S any](a *interact.Action[*model, S]) control {
	return control{action: a, look: lookChevron}
}

// disabledIf is c, greyed and unclickable when off.
func (c control) disabledIf(off bool) control {
	c.off = off
	return c
}

// heading is c, a chevron, drawn with spans after it that a click on folds too.
func (c control) heading(spans ...look.Span) control {
	c.after = spans
	return c
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
		return []look.Span{look.Icon(c.icon)}
	case lookLink:
		return look.Link(c.icon, c.label, key, c.lit)
	case lookQuietCheckbox:
		return look.QuietCheckbox(c.label, key, c.on, c.lit)
	case lookChevron:
		return append([]look.Span{look.Chevron(c.on, c.lit)}, c.after...)
	}
	return look.Button(c.icon, c.label, key)
}

// The controls of rows and the title bar. They are made in init, as kinds are: their actions draw
// rows.
var (
	viewedBox       control
	commitViewedBox control
	fileFold        control
	threadFold      control
	reviewFold      control
	expandFold      control
	commentHere     control
	replyButton     control
	resolveButton   control
	reopenButton    control
	addressedButton control
	unaddressButton control
	newWorkButton   control
	editButton      control
	deleteButton    control
	deleteReview    control
	openInDiff      control
	openInCommit    control
	prevCommit      control
	nextCommit      control
	prevFile        control
	nextFile        control
	filterButton    control
)

func init() {
	// Inside the diff and the overview, controls are quiet; the + under the mouse is the exception.
	viewedBox = quietCheckbox(actMarkFileReviewed, "Viewed")
	commitViewedBox = quietCheckbox(actMarkCommitReviewed, "Viewed")
	fileFold = chevron(actFoldFile)
	threadFold = chevron(actToggleThread)
	reviewFold = chevron(actToggleReview)
	expandFold = link(actExpandFold, look.GlyphNone, "")
	commentHere = icon(actCommentOnLine, look.GlyphComment)
	commentHere.selects = true
	replyButton = link(actReplyToThread, look.GlyphReply, "Reply")
	resolveButton = link(actResolveThread, look.GlyphResolve, "Resolve")
	reopenButton = link(actResolveThread, look.GlyphReopen, "Reopen")
	addressedButton = link(actAddressThread, look.GlyphAddressed, "Addressed")
	unaddressButton = link(actAddressThread, look.GlyphReopen, "Reopen")
	// A button, not a quiet link: it is what to do next, alone on its line.
	newWorkButton = button(actOpenNewWork, look.GlyphNew, "New change")
	editButton = link(actEditComment, look.GlyphEdit, "Edit")
	deleteButton = link(actDeleteComment, look.GlyphDelete, "Delete")
	deleteReview = link(actDeleteReview, look.GlyphDelete, "Delete")
	openInDiff = link(actOpenThreadInDiff, look.GlyphNone, "Open in diff")
	openInCommit = link(actOpenThreadInCommit, look.GlyphNone, "Open in commit")
	prevCommit = button(actPrevCommit, look.GlyphPrev, "Prev")
	nextCommit = button(actNextCommit, look.GlyphNext, "Next")
	prevFile = button(actPrevFile, look.GlyphPrev, "Prev file")
	nextFile = button(actNextFile, look.GlyphNext, "Next file")
	filterButton = button(actFilterThreads, look.GlyphFilter, "")
}

const controlZone = "control:"

// drawnControl is a control on screen: what it runs, the mode it runs in, and whether a press on it
// selects lines first.
type drawnControl struct {
	run     interact.Bound[*model]
	mode    mode
	selects bool
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
			gap := look.Default(" ")
			if c.look >= lookLink {
				// Quiet controls have no chip to set them apart.
				out = append(out, look.Fill(2))
			}
			out = append(out, gap)
		}
		if c.off {
			out = append(out, look.DisabledButton(c.icon, c.label, key)...)
			continue
		}
		out = append(out, m.mark(where, md, b, c.selects, c.spans(key)...)...)
	}
	return out
}

// mark marks spans as a control that runs b in mode md, and records it for a click on where it
// lands. Its zone id names where it is and its action, never its turn in a frame, so a row's
// drawing kept from an earlier frame is still the control it was.
func (m *model) mark(where string, md mode, b interact.Bound[*model], selects bool, spans ...look.Span) []look.Span {
	id := controlZone + where + ":" + b.ID()
	if m.drawnControls == nil {
		m.drawnControls = map[string]drawnControl{}
	}
	m.drawnControls[id] = drawnControl{run: b, mode: md, selects: selects}
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
func (m *model) controlAt(x, y int) (drawnControl, bool) {
	for id, zones := range m.zones {
		c, ok := m.drawnControls[id]
		if !ok || c.mode != m.mode {
			continue
		}
		for _, z := range zones {
			if z.Contains(x, y) {
				return c, true
			}
		}
	}
	return drawnControl{}, false
}
