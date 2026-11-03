package tui

import (
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"

	"charm.land/glamour/v2"
	glamouransi "charm.land/glamour/v2/ansi"
	glamourstyles "charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/termtext"
	"github.com/chancez/gr/internal/tui/look"
)

// renderer draws rows. It caches what depends only on width.
type renderer struct {
	th       *look.Theme
	hl       *highlighter
	markdown map[int]*glamour.TermRenderer
	// segments holds, after renderRow draws a thread, the line each of its comments starts on. The
	// first comment's segment starts at the row's first line, so it includes the excerpt and title.
	segments []int
}

func newRenderer(isDark bool, theme string) *renderer {
	return &renderer{th: look.New(isDark, theme), hl: newHighlighter(isDark), markdown: map[int]*glamour.TermRenderer{}}
}

// rowState is how a row is highlighted.
type rowState struct {
	cursor   bool
	selected bool
	// side is the cursor's side in split mode.
	side anchor.Side
	// dim shows the cursor faintly, when the main pane does not have focus.
	dim bool
	// activeHunk is set on rows of the hunk the cursor is in.
	activeHunk bool
}

// hunkMarker is the one-column margin beside hunk rows: a bar along the hunk the cursor is in.
func (r *renderer) hunkMarker(state rowState) string {
	if state.activeHunk {
		// A solid cell rather than "|", which would merge with the pane divider beside it.
		return r.th.Draw(look.ActiveHunk, 1)
	}
	return " "
}

// cursorSurface is the cursor's surface, faint when the main pane does not have focus.
func cursorSurface(state rowState) look.Surface {
	if state.dim {
		return look.CursorDim
	}
	return look.Cursor
}

// renderMarkdown renders a comment body, falling back to plain text.
func (r *renderer) renderMarkdown(body string, width int) []string {
	width = max(width, 10)
	body = termtext.Block(body)
	md, ok := r.markdown[width]
	if !ok {
		var err error
		md, err = glamour.NewTermRenderer(glamour.WithStyles(markdownStyle(r.th.IsDark())), glamour.WithWordWrap(width))
		if err == nil {
			r.markdown[width] = md
		}
	}
	if md != nil {
		if out, err := md.Render(body); err == nil {
			return trimBlank(strings.Split(strings.TrimRight(out, "\n"), "\n"))
		}
	}
	return look.Wrap(body, width)
}

// markdownStyle is glamour's light or dark style, kept to ASCII.
func markdownStyle(isDark bool) glamouransi.StyleConfig {
	s := glamourstyles.LightStyleConfig
	if isDark {
		s = glamourstyles.DarkStyleConfig
	}
	s.Item.BlockPrefix = "- "
	s.BlockQuote.IndentToken = new("| ")
	// Inline code is padded with non-breaking spaces. The code background marks it already.
	s.Code.Prefix, s.Code.Suffix = "", ""
	return s
}

// trimBlank drops leading and trailing lines that are only whitespace, which glamour adds as margins.
func trimBlank(lines []string) []string {
	blank := func(s string) bool { return strings.TrimSpace(ansi.Strip(s)) == "" }
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", "    ") }

// gutterWidth is the width of the widest line number in a file.
func gutterWidth(f git.FileDiff) int {
	n := 1
	for _, h := range f.Hunks {
		n = max(n, h.OldStart+h.OldLines, h.NewStart+h.NewLines)
	}
	return len(strconv.Itoa(n))
}

// renderRow draws one row at width. d supplies thread draft state.
func (r *renderer) renderRow(d *changeData, v view, rw row, width int, state rowState) []string {
	r.segments = nil
	switch rw.kind {
	case rowText:
		return rw.text(width)
	case rowBoxTop:
		return []string{r.boxEdge(width)}
	case rowCommit:
		return r.renderCommit(v, width, state)
	case rowFile:
		f := v.files[rw.file]
		surf := look.HeadingBar
		if state.cursor {
			surf = cursorSurface(state)
		}
		spans := []look.Span{look.File(f)}
		if size, ok := diffSize(f); ok {
			spans = append(append(spans, look.Default("  ")), size...)
		}
		if rw.reviewed {
			spans = append(spans, look.Default("  "), look.Reviewed())
		}
		switch {
		case rw.collapsed:
			spans = append(append(spans, look.Default("  "), look.Muted("collapsed, ")), look.Hint("enter", "expands")...)
		case rw.reviewed:
			spans = append(append(spans, look.Default("  ")), look.Hint("enter", "collapses")...)
		}
		return []string{r.th.Draw(surf, width, spans...)}
	case rowHunk:
		surf := look.HunkBar
		if state.cursor {
			surf = cursorSurface(state)
		}
		return []string{r.hunkMarker(state) + r.th.Draw(surf, width-1, look.Hunk(*rw.hunk))}
	case rowLine:
		f := v.files[rw.file]
		if isMessage(f) {
			return []string{r.renderMessageLine(*rw.line, width, state)}
		}
		if rw.line != nil {
			return []string{r.hunkMarker(state) + r.renderUnified(f, *rw.line, width-1, state)}
		}
		return []string{r.hunkMarker(state) + r.renderSplit(f, rw.left, rw.right, width-1, state)}
	case rowThread:
		if rw.file >= 0 && isMessage(v.files[rw.file]) {
			// A thread on the message sits inside the commit's box, under its line.
			inner := rw
			inner.file = -1
			return r.boxed(r.renderRow(d, v, inner, width-4, state), width)
		}
		if rw.collapsed {
			return []string{r.renderCollapsedThread(d, *rw.thread, width, state)}
		}
		lines, starts := r.renderThread(d, *rw.thread, width, state.cursor)
		shift := 0
		if rw.excerpt != nil {
			excerpt := r.renderExcerpt(*rw.excerpt, rw.thread.Location, width)
			lines, shift = append(excerpt, lines...), len(excerpt)
		}
		r.segments = []int{0}
		for _, s := range starts[1:] {
			r.segments = append(r.segments, s+shift)
		}
		return lines
	case rowReview:
		return r.renderReview(d, rw, width, state)
	case rowGap:
		label := "   " + gapLabel(*rw.gap) + "  (zo or enter to expand)"
		surf := look.Plain
		if state.cursor {
			surf = cursorSurface(state)
		}
		return []string{" " + r.th.Draw(surf, width-1, look.Muted(label))}
	}
	return nil
}

// lineSurface is where a diff line sits: the cursor or selection on the cursor's side, or its
// added or deleted color.
func lineSurface(kind git.LineKind, state rowState, cursorSide bool) look.Surface {
	switch {
	case state.cursor && cursorSide:
		return cursorSurface(state)
	case state.selected && cursorSide:
		return look.Selected
	case kind == git.LineAdded:
		return look.AddedLine
	case kind == git.LineDeleted:
		return look.DeletedLine
	}
	return look.Plain
}

// code is a line's text, highlighted.
func (r *renderer) code(f git.FileDiff, l git.Line) look.Span {
	return look.Styled(func(bg color.Color) string { return r.hl.line(f.Path(), expandTabs(l.Text), bg) })
}

func (r *renderer) renderUnified(f git.FileDiff, l git.Line, width int, state rowState) string {
	return r.th.Draw(lineSurface(l.Kind, state, true), width,
		look.Gutter(gutterWidth(f), l.OldLine, l.NewLine), look.LineMark(l.Kind), look.Default(" "), r.code(f, l))
}

func (r *renderer) renderSplit(f git.FileDiff, left, right *git.Line, width int, state rowState) string {
	gw := gutterWidth(f)
	half := (width - 1) / 2
	cell := func(l *git.Line, side anchor.Side, w int) string {
		if l == nil {
			return r.th.Draw(look.Plain, w)
		}
		n := l.NewLine
		if side == anchor.SideOld {
			n = l.OldLine
		}
		return r.th.Draw(lineSurface(l.Kind, state, state.side == side), w,
			look.Gutter(gw, n), look.LineMark(l.Kind), look.Default(" "), r.code(f, *l))
	}
	return cell(left, anchor.SideOld, half) + r.th.Render(look.Plain, look.Border("|", false)) + cell(right, anchor.SideNew, width-half-1)
}

// The commit's header is a box, as GitHub draws it: the message's lines, the subject strong, each
// a row the cursor can be on, then a footer of who committed it and when, its size, and its sha.
// A message's threads sit inside the box, under their lines.

// boxed puts lines inside the box's sides.
func (r *renderer) boxed(lines []string, width int) []string {
	side := r.th.Render(look.Plain, look.Border("|", false))
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = side + " " + look.Fit(l, max(width-4, 0)) + " " + side
	}
	return out
}

func (r *renderer) boxEdge(width int) string {
	return r.th.Render(look.Plain, look.Border("+"+strings.Repeat("-", max(width-2, 0))+"+", false))
}

// renderMessageLine draws line l of a commit's message in the box.
func (r *renderer) renderMessageLine(l git.Line, width int, state rowState) string {
	el := look.Text
	if l.NewLine == 1 {
		el = look.Heading
	}
	surf := look.Plain
	switch {
	case state.cursor:
		surf = cursorSurface(state)
	case state.selected:
		surf = look.Selected
	}
	return r.boxed([]string{r.th.DrawCut(surf, max(width-4, 0), el(termtext.Line(l.Text)))}, width)[0]
}

// renderCommit draws the box's footer and its bottom.
func (r *renderer) renderCommit(v view, width int, state rowState) []string {
	c := v.commit
	inner := max(width-4, 10)
	surf := look.Plain
	if state.cursor {
		surf = cursorSurface(state)
	}
	by := look.CommitBy(c)
	which := append(filesSize(slices.DeleteFunc(slices.Clone(v.files), isMessage)), look.Muted("  commit "), look.SHA(c.SHA).Short())
	var foot []string
	if gap := inner - ansi.StringWidth(look.TextOf(by...)) - ansi.StringWidth(look.TextOf(which...)); gap > 0 {
		foot = []string{r.th.Draw(surf, inner, append(append(by, look.Default(strings.Repeat(" ", gap))), which...)...)}
	} else {
		foot = []string{r.th.Draw(surf, inner, by...), r.th.Draw(surf, inner, which...)}
	}
	return append(r.boxed(append([]string{""}, foot...), width), r.boxEdge(width))
}

// commentHeader is a comment's author and time, and whether it is a draft.
func commentHeader(d *changeData, c store.Comment) []look.Span {
	out := []look.Span{look.Author(c.Author), look.Default("  "), look.Time(c.CreatedAt)}
	if d.isDraft(c.ReviewID) {
		out = append(out, look.Draft("  draft"))
	}
	return out
}

// renderExcerpt draws the diff a thread is about, under a line naming where it is.
func (r *renderer) renderExcerpt(e review.Excerpt, loc store.Location, width int) []string {
	where := append([]look.Span{look.Default(" ")}, look.MarkSHA(commitMentions.Zone(loc.Commit), look.Location(loc, look.Strong)...)...)
	if !e.Exact {
		where = append(where, look.Muted("  (outdated: as it was)"))
	}
	out := []string{r.th.Draw(look.HeadingBar, width, where...)}
	// A one-hunk file sized to the excerpt's last line numbers, so the gutter is as wide as needed.
	f := git.FileDiff{NewPath: e.Path}
	if n := len(e.Lines); n > 0 {
		last := e.Lines[n-1]
		f.Hunks = []git.Hunk{{OldStart: last.OldLine, NewStart: last.NewLine}}
	}
	for _, l := range e.Lines {
		out = append(out, " "+r.renderUnified(f, l, width-1, rowState{}))
	}
	return out
}

// renderReview draws a review's header bar and, unless collapsed, its summary and the replies to it
// beneath, unboxed, so a summary reads as part of its review rather than as one more thread.
func (r *renderer) renderReview(d *changeData, rw row, width int, state rowState) []string {
	lines := rw.text(width)
	t := rw.thread
	if t == nil {
		return lines
	}
	if rw.collapsed {
		first := ""
		if len(t.Comments) > 0 {
			first = strings.Join(strings.Fields(t.Comments[0].Body), " ")
		}
		line := fmt.Sprintf("    %d comments  %s", len(t.Comments), first)
		return append(lines, r.th.DrawCut(look.Plain, width, look.Muted(line)))
	}
	r.segments = []int{0}
	for i, c := range t.Comments {
		if i > 0 {
			r.segments = append(r.segments, len(lines))
			lines = append(lines, "")
			lines = append(lines, r.th.Draw(look.Plain, width, append([]look.Span{look.Default("    ")}, commentHeader(d, c)...)...))
		}
		for _, l := range r.renderMarkdown(c.Body, width-6) {
			lines = append(lines, "    "+l)
		}
	}
	return lines
}

// renderCollapsedThread draws a folded thread on one line, as GitHub shows resolved threads.
func (r *renderer) renderCollapsedThread(d *changeData, t review.ThreadView, width int, state rowState) string {
	where := look.MarkSHA(commitMentions.Zone(t.Location.Commit), look.Location(t.Location)...)
	if t.Summary {
		where = []look.Span{look.Muted("summary")}
	}
	first := ""
	if len(t.Comments) > 0 {
		first = t.Comments[0].Author.Name + ": " + strings.Join(strings.Fields(t.Comments[0].Body), " ")
	}
	spans := []look.Span{look.Muted("  > " + t.Label() + " ")}
	spans = append(spans, look.State(t, d.isDraft(t.ReviewID))...)
	spans = append(spans, look.Muted("  "))
	spans = append(spans, where...)
	spans = append(spans, look.Muted(fmt.Sprintf("  %d comments  %s", len(t.Comments), first)))
	surf := look.Plain
	if state.cursor {
		surf = cursorSurface(state)
	}
	return r.th.DrawCut(surf, width, look.DimIf(t.State == store.ThreadResolved, spans...)...)
}

// renderThread draws a thread as a bordered box, and returns the line each comment starts on.
func (r *renderer) renderThread(d *changeData, t review.ThreadView, width int, cursor bool) ([]string, []int) {
	const indent = 2
	boxWidth := max(width-indent, 20)
	inner := boxWidth - 4
	title := []look.Span{look.Ref(t), look.Default(" ")}
	if t.Summary {
		// A summary belongs to its review, so it is titled as the review's rather than numbered.
		title = append(look.SummaryTitle(t), look.Default(" "))
	}
	title = append(title, look.State(t, d.isDraft(t.ReviewID))...)
	if loc := look.MarkSHA(commitMentions.Zone(t.Location.Commit), look.Location(t.Location)...); len(loc) > 0 && !t.Summary {
		title = append(append(title, look.Muted("  ")), loc...)
	}
	// A resolved thread recedes: its title and comment headers are dimmed.
	done := t.State == store.ThreadResolved
	lines := []string{ansi.Truncate(r.th.Render(look.Plain, look.DimIf(done, title...)...), inner, "")}
	var starts []int
	for _, c := range t.Comments {
		// The box's top border is the first output line.
		starts = append(starts, len(lines)+1)
		lines = append(lines, "", r.th.Render(look.Plain, look.DimIf(done, commentHeader(d, c)...)...))
		lines = append(lines, r.renderMarkdown(c.Body, inner)...)
	}
	out := strings.Split(r.th.Box(boxWidth, cursor).Render(strings.Join(lines, "\n")), "\n")
	for i := range out {
		out[i] = strings.Repeat(" ", indent) + out[i]
	}
	if len(starts) == 0 {
		starts = []int{0}
	}
	return out, starts
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// isMessage reports whether a file is the commit message.
func isMessage(f git.FileDiff) bool { return f.NewPath == review.CommitMsgPath }
