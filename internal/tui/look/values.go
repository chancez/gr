package look

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// Constructors for each kind of value gr draws. Each takes the value itself, so the element and
// the format come from its type.

// SHA is a commit's sha.
type SHA string

// Short is the sha abbreviated to 7 characters, as in a commit list.
func (s SHA) Short() Span {
	if len(s) > 7 {
		s = s[:7]
	}
	return sp(elSHA, string(s))
}

// Full is the whole sha.
func (s SHA) Full() Span {
	return sp(elSHA, string(s))
}

// Author is who wrote a comment, a review, or a commit.
func Author(a store.Author) Span {
	return sp(elAuthor, a.Name)
}

// Time is when something happened, in local time.
func Time(t time.Time) Span {
	return sp(elMuted, t.Local().Format("2006-01-02 15:04"))
}

// CommitBy is who made a commit and when, as GitHub's commit footer has it.
func CommitBy(c git.Commit) []Span {
	return []Span{Author(store.Author{Name: c.AuthorName}), sp(elMuted, " committed "), Time(c.AuthorTime)}
}

// FileLabel is a file's path, or its old and new paths when renamed.
func FileLabel(f git.FileDiff) string {
	if f.OldPath != "" && f.NewPath != "" && f.OldPath != f.NewPath {
		return f.OldPath + " -> " + f.NewPath
	}
	return f.Path()
}

// File is a file's path as a file header shows it: after its status letter, and with notes for a
// binary file or a changed mode.
func File(f git.FileDiff) Span {
	label := " " + StatusLetter(f.Status) + " " + FileLabel(f)
	if f.Binary {
		label += "  (binary)"
	}
	if f.OldMode != "" && f.NewMode != "" && f.OldMode != f.NewMode {
		label += fmt.Sprintf("  (mode %s -> %s)", f.OldMode, f.NewMode)
	}
	return sp(elPath, label)
}

// StatusLetter is a file's status as git's --name-status writes it.
func StatusLetter(s git.FileStatus) string {
	switch s {
	case git.StatusAdded:
		return "A"
	case git.StatusDeleted:
		return "D"
	case git.StatusRenamed:
		return "R"
	case git.StatusCopied:
		return "C"
	}
	return "M"
}

// Location names where a thread points: the commit's sha, then the path and lines. Thread titles
// show it muted; prop Strong shows it as a heading, as over an excerpt.
func Location(l store.Location, p ...Prop) []Span {
	el := elMuted
	if len(p) > 0 && p[0]&Strong != 0 {
		el = elPath
	}
	var out []Span
	if l.Scope == store.ScopeChange {
		return nil
	}
	if l.Scope == store.ScopeCommit {
		out = append(out, sp(el, "commit "), SHA(l.Commit).Short())
	}
	if l.Path != "" {
		loc := l.Path
		if loc == review.CommitMsgPath {
			// The message is anchored as a file, but shown as the commit's.
			loc = "message"
		}
		if l.StartLine > 0 {
			loc += ":" + strconv.Itoa(l.StartLine)
			if l.EndLine != l.StartLine {
				loc += "-" + strconv.Itoa(l.EndLine)
			}
		}
		if l.Side == anchor.SideOld {
			loc += " (old)"
		}
		if len(out) > 0 {
			loc = " " + loc
		}
		out = append(out, sp(el, loc))
	}
	return out
}

// Ref is a thread's label, such as "#3" or "r1".
func Ref(t review.ThreadView) Span {
	return sp(elRef, t.Label())
}

// SummaryTitle titles a review's summary thread as the review's, with its ref.
func SummaryTitle(t review.ThreadView) []Span {
	return []Span{sp(elRef, "Summary"), sp(elMuted, " ("+t.Label()+")")}
}

// State is a thread's state badge: its state and who set it, outdated, and draft.
func State(t review.ThreadView, draft bool) []Span {
	var out []Span
	switch t.State {
	case store.ThreadOpen:
		out = append(out, glyph(elStateOpen, GlyphStateOpen, true), sp(elStateOpen, "open"))
	case store.ThreadAddressed:
		out = append(out, glyph(elStateAddressed, GlyphStateAddressed, true), sp(elStateAddressed, "addressed"))
	case store.ThreadResolved:
		out = append(out, glyph(elStateResolved, GlyphStateResolved, true), sp(elStateResolved, "resolved"))
	}
	if t.State != store.ThreadOpen && t.StateBy.Name != "" {
		out = append(out, sp(elMuted, " by "+t.StateBy.Name))
	}
	if t.Outdated {
		out = append(out, sp(elMuted, ", "), sp(elOutdated, "outdated"))
	}
	if draft {
		out = append(out, sp(elMuted, ", "), sp(elDraft, "draft"))
	}
	return out
}

// StateCount is how many threads are in a state, such as "3 open".
func StateCount(s store.ThreadState, n int) Span {
	el := elStateOpen
	switch s {
	case store.ThreadAddressed:
		el = elStateAddressed
	case store.ThreadResolved:
		el = elStateResolved
	}
	return sp(el, fmt.Sprintf("%d %s", n, s))
}

// Verdict is a review's verdict, in its own color.
func Verdict(v store.Verdict) Span {
	switch v {
	case store.VerdictApprove:
		return sp(elVerdictApprove, "approved")
	case store.VerdictRequestChanges:
		return sp(elVerdictRequestChanges, "requested changes")
	}
	return sp(elVerdictComment, "commented")
}

// Hunk is a hunk's header range and section.
func Hunk(h git.Hunk) Span {
	return sp(elHunkRange, fmt.Sprintf(" @@ -%d,%d +%d,%d @@ %s", h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Section))
}

// LineMark is a diff line's "+" or "-", or a space for context.
func LineMark(k git.LineKind) Span {
	switch k {
	case git.LineAdded:
		return sp(elMarkAdded, "+")
	case git.LineDeleted:
		return sp(elMarkDeleted, "-")
	}
	return sp(elDefault, " ")
}

// DiffSize is how many lines a diff adds and deletes, "+12 -3", as GitHub shows it. A binary file
// has none to show.
func DiffSize(added, deleted int) []Span {
	return []Span{sp(elMarkAdded, fmt.Sprintf("+%d", added)), sp(elDefault, " "), sp(elMarkDeleted, fmt.Sprintf("-%d", deleted))}
}

// Gutter is a diff line's numbers, each right-aligned to width and followed by a space. Zero is a
// line that is not on that side.
func Gutter(width int, lines ...int) Span {
	var b strings.Builder
	for _, n := range lines {
		if n == 0 {
			b.WriteString(strings.Repeat(" ", width))
		} else {
			fmt.Fprintf(&b, "%*d", width, n)
		}
		b.WriteByte(' ')
	}
	return sp(elGutter, b.String())
}

// Badge is an entry's open thread count and its marks.
func Badge(open int, draft, reviewed bool) []Span {
	var out []Span
	if open > 0 {
		out = append(out, sp(elCount, fmt.Sprintf("(%d)", open)))
	}
	if draft || reviewed {
		if len(out) > 0 {
			out = append(out, sp(elDefault, " "))
		}
		if draft {
			out = append(out, glyph(elDraft, GlyphDraft, false).With(Strong))
		}
		if reviewed {
			out = append(out, glyph(elReviewed, GlyphReviewed, false).With(Strong))
		}
	}
	return out
}

// Reviewed is the "reviewed" label on a file reviewed in full.
func Reviewed() Span {
	return sp(elReviewed, "reviewed").With(Strong)
}

// Draft is text about drafts not yet submitted, such as a pending review's count.
func Draft(s string) Span {
	return sp(elDraft, s)
}

// Key is a key, as help shows it.
func Key(k string) Span {
	return sp(elKey, k)
}

// Hint is a key and what it does.
func Hint(k, desc string) []Span {
	return []Span{Key(k), sp(elDefault, " "), sp(elKeyDesc, desc)}
}

// ClickableHint is a hint a click runs, its key underlined as a link.
func ClickableHint(k, desc string) []Span {
	return []Span{Key(k).With(Underlined), sp(elDefault, " "), sp(elKeyDesc, desc)}
}

// Controls are chips: their label and key on a background of their own, as GitHub's buttons, so
// what a click runs looks the same everywhere.

func chip(spans ...Span) []Span {
	out := make([]Span, len(spans))
	for i, x := range spans {
		x.chip = true
		out[i] = x
	}
	return out
}

// Button is a control: its icon and label, and the key that does the same.
func Button(icon Glyph, label, key string) []Span {
	out := []Span{sp(elDefault, " "), glyph(elButton, icon, true), sp(elButton, label)}
	if key != "" {
		out = append(out, sp(elDefault, " "), sp(elMuted, key))
	}
	return chip(append(out, sp(elDefault, " "))...)
}

// DisabledButton is a button that cannot be clicked now, as Next on the last commit: drawn in place,
// greyed, so the controls around it stay put and it can be seen to be there.
func DisabledButton(icon Glyph, label, key string) []Span {
	out := []Span{sp(elDefault, " "), glyph(elMuted, icon, true), sp(elMuted, label)}
	if key != "" {
		out = append(out, sp(elDefault, " "), sp(elMuted, key))
	}
	return chip(append(out, sp(elDefault, " "))...)
}

// Checkbox is a control that shows a state, checked when on: its label, and the key that toggles
// it.
func Checkbox(label, key string, on bool) []Span {
	box := glyph(elButton, GlyphUnchecked, true)
	if on {
		box = glyph(elButton, GlyphChecked, true)
	}
	out := []Span{sp(elDefault, " "), box, sp(elButton, label)}
	if key != "" {
		out = append(out, sp(elDefault, " "), sp(elMuted, key))
	}
	return chip(append(out, sp(elDefault, " "))...)
}

// Icon is a control drawn as one symbol that stands out on any line, such as the + that comments
// on a line.
func Icon(g Glyph) Span {
	x := glyph(elKey, g, false)
	x.solid = true
	return x
}

// Controls inside content, as a diff's, are quiet: their label muted, without a chip or a key,
// until lit, as on the row under the mouse or the cursor, when they are underlined and show their
// key, so the diff stays the thing to read.

// Link is a quiet control: its icon and label, and its key when lit. Unlit, the key's room is kept
// blank, so lighting it, as the mouse reaches it, moves nothing after it.
func Link(icon Glyph, label, key string, lit bool) []Span {
	if !lit {
		out := []Span{glyph(elMuted, icon, true), sp(elMuted, label)}
		if key != "" {
			out = append(out, sp(elDefault, strings.Repeat(" ", 1+ansi.StringWidth(key))))
		}
		return out
	}
	out := []Span{glyph(elKey, icon, true), sp(elText, label).With(Underlined)}
	if key != "" {
		out = append(out, sp(elDefault, " "), sp(elMuted, key))
	}
	return out
}

// QuietCheckbox is a quiet control that shows a state: its box, in the reviewed color when on, and
// its label as a Link.
func QuietCheckbox(label, key string, on, lit bool) []Span {
	box := glyph(elMuted, GlyphUnchecked, true)
	if on {
		box = glyph(elReviewed, GlyphChecked, true)
	}
	return append([]Span{box}, Link(GlyphNone, label, key, lit)...)
}

// Chevron is a quiet control that folds what it heads, pointing down when open, in the accent
// when lit.
func Chevron(open, lit bool) Span {
	g := GlyphFolded
	if open {
		g = GlyphOpen
	}
	if lit {
		return glyph(elKey, g, false)
	}
	return glyph(elMuted, g, false)
}

// More marks entries of a list scrolled out of view, n of them, above or below.
func More(above bool, n int) []Span {
	g := GlyphMoreBelow
	if above {
		g = GlyphMoreAbove
	}
	return []Span{glyph(elMuted, g, false), sp(elMuted, fmt.Sprintf(" %d more", n))}
}

// Free text, styled by what it is for.

// Default is text in the terminal's own foreground.
func Default(s string) Span { return sp(elDefault, s) }

// Text is text in the theme's foreground.
func Text(s string) Span { return sp(elText, s) }

// Muted is secondary text, such as notes and counts.
func Muted(s string) Span { return sp(elMuted, s) }

// Heading is a title.
func Heading(s string) Span { return sp(elHeading, s) }

// KeyDesc is what a key does.
func KeyDesc(s string) Span { return sp(elKeyDesc, s) }

// Mode marks a mode that changes what keys do, such as selecting lines.
func Mode(s string) Span { return sp(elMode, s) }

// Warning is a caution, such as editing a resolved thread.
func Warning(s string) Span { return sp(elWarning, s) }

// Error is an error or a destructive prompt.
func Error(s string) Span { return sp(elError, s) }

// Border is a divider, in the accent when active.
func Border(s string, active bool) Span {
	if active {
		return sp(elBorderActive, s)
	}
	return sp(elBorder, s)
}
