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

func newRenderer(isDark bool, theme string, glyphs look.Glyphs) *renderer {
	th := look.New(isDark, theme)
	th.SetGlyphs(glyphs)
	return &renderer{th: th, hl: newHighlighter(isDark), markdown: map[int]*glamour.TermRenderer{}}
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
	// split is set in split mode.
	split bool
	// focusOld and focusNew are set on a line the thread under the cursor or mouse is on, on that
	// side, which tints it.
	focusOld bool
	focusNew bool
	// hover is set on the row under the mouse, hoverSide the side of a split diff it is over.
	hover     bool
	hoverSide anchor.Side
	// controls are the row's controls, drawn after what they act on: a line's in place of its
	// +/- sign, and a fold's in place of its label. lead are those before it, as a file's chevron.
	controls []look.Span
	lead     []look.Span
	// below are drawn on a line of their own under the row, as an open review summary's buttons.
	below []look.Span
	// trail are drawn at the end of an open thread's header line in the overview.
	trail []look.Span
	// code names a diff line's code, for the mouse to find on screen: its zone is code, then the
	// side, as codeZone has it.
	code string
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
		// The chevron, when there is one, heads the file's name and folds from either.
		spans := []look.Span{look.File(f)}
		if len(state.lead) > 0 {
			spans = state.lead
		}
		if size, ok := diffSize(f); ok {
			spans = append(append(spans, look.Default("  ")), size...)
		}
		if rw.collapsed {
			spans = append(spans, look.Default("  "), look.Muted("collapsed"))
		}
		if len(state.controls) > 0 {
			spans = append(append(spans, look.Default("  ")), state.controls...)
		}
		return []string{r.th.DrawCut(surf, width, spans...)}
	case rowHunk:
		surf := look.HunkBar
		if state.cursor {
			surf = cursorSurface(state)
		}
		h := rw.hunk
		if rw.header != nil {
			h = rw.header
		}
		return []string{r.hunkMarker(state) + r.th.Draw(surf, width-1, look.Hunk(*h))}
	case rowLine:
		f := v.files[rw.file]
		if isMessage(f) {
			return []string{r.renderMessageLine(*rw.line, rw.coverNew, width, state)}
		}
		if rw.line != nil {
			return []string{r.hunkMarker(state) + r.renderUnified(f, *rw.line, width-1, state,
				commented(append(slices.Clip(rw.coverOld), rw.coverNew...)), state.focusOld || state.focusNew)}
		}
		return []string{r.hunkMarker(state) + r.renderSplit(f, rw, width-1, state)}
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
		if rw.excerpt != nil {
			// In the overview, a thread is headed by its line, as when folded, then the code it is on,
			// then its comments, which its header names.
			head := append([]string{r.threadHeader(d, *rw.thread, width, state, true)}, r.renderExcerpt(*rw.excerpt, *rw.thread, width)...)
			lines, starts := r.renderThread(d, *rw.thread, width, state, threadIndent(v, rw, state.split, width), false)
			r.segments = []int{0}
			for _, s := range starts[1:] {
				r.segments = append(r.segments, s+len(head))
			}
			return append(head, lines...)
		}
		lines, starts := r.renderThread(d, *rw.thread, width, state, threadIndent(v, rw, state.split, width), true)
		shift := 0
		r.segments = []int{0}
		for _, s := range starts[1:] {
			r.segments = append(r.segments, s+shift)
		}
		return lines
	case rowReview:
		return r.renderReview(d, rw, width, state)
	case rowGap:
		surf := look.Plain
		if state.cursor {
			surf = cursorSurface(state)
		}
		spans := []look.Span{look.Default("   "), look.Muted(gapLabel(*rw.gap))}
		if len(state.controls) > 0 {
			spans = append(append(spans, look.Default("   ")), state.controls...)
		}
		return []string{" " + r.th.Draw(surf, width-1, spans...)}
	}
	return nil
}

// lineSurface is where a diff line sits: the cursor or selection on the cursor's side, or its
// added or deleted color.
func lineSurface(kind git.LineKind, state rowState, cursorSide, commented, focused bool) look.Surface {
	switch {
	case state.cursor && cursorSide:
		return cursorSurface(state)
	case state.selected && cursorSide, focused:
		return look.Selected
	case commented:
		// A thread's lines show as its, over their added or deleted color, as GitHub highlights a
		// range; the sign still says which they are.
		return look.Commented
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

// renderUnified draws a line, tinted when threads are on it, and as a selection when the thread
// under the cursor or the mouse is.
func (r *renderer) renderUnified(f git.FileDiff, l git.Line, width int, state rowState, tint, focused bool) string {
	return r.th.Draw(lineSurface(l.Kind, state, true, tint, focused), width,
		append(append([]look.Span{gutter(gutterWidth(f), tint, l.OldLine, l.NewLine)}, lineSign(l.Kind, state.controls)...),
			look.Default(" "), r.markCode(state, sideUnified, r.code(f, l)))...)
}

// sideUnified is a unified diff line's code, as against one side of a split diff's.
const sideUnified = "unified"

// codeZone is the zone a diff line's code on side is drawn in, under the row's name.
func codeZone(row, side string) string { return row + ":" + side }

// markCode marks a line's code as the row's, on side, for the mouse to find.
func (r *renderer) markCode(state rowState, side string, code look.Span) look.Span {
	if state.code == "" {
		return code
	}
	return look.Mark(codeZone(state.code, side), code)[0]
}

// commented reports whether threads that are not resolved are on a line, which tints its line
// numbers. Resolved threads fold away, and their lines are left plain.
func commented(covers []cover) bool {
	return slices.ContainsFunc(covers, func(c cover) bool { return c.thread.State != store.ThreadResolved })
}

// gutter is a line's numbers, tinted when threads are on it.
func gutter(width int, tint bool, lines ...int) look.Span {
	g := look.Gutter(width, lines...)
	if tint {
		return look.OnCommented(g)
	}
	return g
}

// lineSign is a line's +/- sign, or the control drawn in its place, as the + that comments on the line
// under the mouse.
func lineSign(kind git.LineKind, controls []look.Span) []look.Span {
	if len(controls) > 0 {
		return controls
	}
	return []look.Span{look.LineMark(kind)}
}

func (r *renderer) renderSplit(f git.FileDiff, rw row, width int, state rowState) string {
	left, right := rw.left, rw.right
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
		var controls []look.Span
		if state.hoverSide == side {
			controls = state.controls
		}
		covers, focused := rw.coverNew, state.focusNew
		if side == anchor.SideOld {
			covers, focused = rw.coverOld, state.focusOld
		}
		return r.th.Draw(lineSurface(l.Kind, state, state.side == side, commented(covers), focused), w,
			append(append([]look.Span{gutter(gw, commented(covers), n)}, lineSign(l.Kind, controls)...), look.Default(" "),
				r.markCode(state, string(side), r.code(f, *l)))...)
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
func (r *renderer) renderMessageLine(l git.Line, covered []cover, width int, state rowState) string {
	el := look.Text
	if l.NewLine == 1 {
		el = look.Heading
	}
	surf := look.Plain
	switch {
	case state.cursor:
		surf = cursorSurface(state)
	case state.selected, state.focusNew:
		surf = look.Selected
	case commented(covered):
		surf = look.Commented
	}
	line := r.boxed([]string{r.th.DrawCut(surf, max(width-4, 0), el(termtext.Line(l.Text)))}, width)[0]
	// The + that comments on the line takes the box's margin, as it takes a diff line's sign, or else
	// the margin is tinted when threads are on the line, as a diff line's numbers are.
	margin := ""
	switch {
	case len(state.controls) > 0:
		margin = r.th.Draw(look.ActiveHunk, 1, state.controls...)
	case commented(covered):
		margin = r.th.Render(look.Plain, look.OnCommented(look.Default(" ")))
	}
	if margin != "" {
		side := r.th.Render(look.Plain, look.Border("|", false))
		line = side + margin + strings.TrimPrefix(line, side+" ")
	}
	return line
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
	if len(state.controls) > 0 {
		which = append(append(which, look.Default("  ")), state.controls...)
	}
	var foot []string
	if gap := inner - ansi.StringWidth(r.th.TextOf(by...)) - ansi.StringWidth(r.th.TextOf(which...)); gap > 0 {
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

// maxBoxWidth caps a thread's or the comment box's width, so on a wide display a box is its own
// beside the code rather than spanning the pane.
const maxBoxWidth = 120

// boxWidthIn is the width of a box indented by indent in a pane width wide: up to maxBoxWidth, and
// on a narrower pane as wide as it, less a column, so the box always has a border of its own.
func boxWidthIn(width, indent int) int {
	return max(min(width-indent-1, maxBoxWidth), 24)
}

// threadName is what a thread is called: its number, or a summary its review's.
func threadName(t review.ThreadView) []look.Span {
	if t.Summary {
		// A summary belongs to its review, so it is titled as the review's rather than numbered.
		return look.SummaryTitle(t)
	}
	return []look.Span{look.Ref(t)}
}

// threadIndent is how far a thread's box is indented in a pane width wide: to where the code
// starts, past the line numbers, for a thread on lines, so the box hangs off the lines its numbers
// are tinted on, on its side in split mode, and a little for one on a file, a commit, or the
// change.
func threadIndent(v view, rw row, split bool, width int) int {
	t := rw.thread
	if t == nil || t.Location.StartLine == 0 {
		return 2
	}
	return codeIndent(v, rw, split, t.Location.Side, width)
}

// codeIndent is the column the code of row rw's file, or of its excerpt, starts at, on side in
// split mode, in a pane width wide.
func codeIndent(v view, rw row, split bool, side anchor.Side, width int) int {
	switch {
	case rw.excerpt != nil:
		// The excerpt's margin, both its numbers, its sign, and the space after.
		return 1 + 2*(excerptGutterWidth(*rw.excerpt)+1) + 2
	case rw.file >= 0 && rw.file < len(v.files) && split:
		// The hunk marker, one side's number, its sign, and the space after, past the old side and
		// the divider for the new.
		left := 1 + gutterWidth(v.files[rw.file]) + 1 + 2
		if side == anchor.SideOld {
			return left
		}
		// renderSplit halves the pane less the hunk marker.
		return 1 + (width-2)/2 + 1 + left - 1
	case rw.file >= 0 && rw.file < len(v.files):
		return 1 + 2*(gutterWidth(v.files[rw.file])+1) + 2
	}
	return 2
}

// excerptGutterWidth is how wide an excerpt's line numbers are: as wide as its last line's.
func excerptGutterWidth(e review.Excerpt) int {
	return gutterWidth(excerptFile(e))
}

// excerptFile is a one-hunk file sized to an excerpt's last line numbers, so its gutter is as
// wide as needed.
func excerptFile(e review.Excerpt) git.FileDiff {
	f := git.FileDiff{NewPath: e.Path}
	if n := len(e.Lines); n > 0 {
		last := e.Lines[n-1]
		f.Hunks = []git.Hunk{{OldStart: last.OldLine, NewStart: last.NewLine}}
	}
	return f
}

// renderExcerpt draws the diff a thread is about, which the thread's header names.
func (r *renderer) renderExcerpt(e review.Excerpt, t review.ThreadView, width int) []string {
	loc := t.Location
	var out []string
	f := excerptFile(e)
	for _, l := range e.Lines {
		// The thread's own lines carry its bar, among the context around them.
		n := l.NewLine
		if loc.Side == anchor.SideOld {
			n = l.OldLine
		}
		end := max(loc.EndLine, loc.StartLine)
		tint := t.State != store.ThreadResolved && n > 0 && n >= loc.StartLine && n <= end
		out = append(out, " "+r.renderUnified(f, l, width-1, rowState{}, tint, false))
	}
	return out
}

// renderReview draws a review's header bar and, unless collapsed, its summary and the replies to it
// beneath, unboxed, so a summary reads as part of its review rather than as one more thread.
func (r *renderer) renderReview(d *changeData, rw row, width int, state rowState) []string {
	// The bar's parts apart, its first headed by the chevron that folds the summary, when there is
	// one, and its controls after them.
	parts := rw.bar
	if len(parts) > 0 {
		// A bar with no summary to fold keeps the chevron's room, so the reviews' names line up.
		head := append([]look.Span{look.Default("  ")}, parts[0]...)
		if len(state.lead) > 0 {
			head = state.lead
		}
		parts = append([][]look.Span{head}, parts[1:]...)
	}
	spans := []look.Span{look.Default(" ")}
	for i, p := range parts {
		if i > 0 {
			spans = append(spans, look.Default("  "))
		}
		spans = append(spans, p...)
	}
	spans = look.DimIf(rw.done, spans...)
	if len(state.controls) > 0 {
		spans = append(append(spans, look.Default("  ")), state.controls...)
	}
	lines := []string{r.th.DrawCut(look.HeadingBar, width, spans...)}
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
	if len(state.below) > 0 {
		lines = append(lines, r.th.DrawCut(look.Plain, width, append([]look.Span{look.Default("      ")}, state.below...)...))
	}
	return lines
}

// renderCollapsedThread draws a folded thread on one line, as GitHub shows resolved threads.
func (r *renderer) renderCollapsedThread(d *changeData, t review.ThreadView, width int, state rowState) string {
	return r.threadHeader(d, t, width, state, false)
}

// threadHeader is a thread's line in the overview, the same folded or open: its chevron, number,
// state, where it is, and how many comments it has, and while folded the first of them.
func (r *renderer) threadHeader(d *changeData, t review.ThreadView, width int, state rowState, open bool) string {
	where := look.MarkSHA(commitMentions.Zone(t.Location.Commit), look.Location(t.Location)...)
	if t.Summary {
		where = []look.Span{look.Muted("summary")}
	}
	first := ""
	if len(t.Comments) > 0 && !open {
		first = t.Comments[0].Author.Name + ": " + strings.Join(strings.Fields(t.Comments[0].Body), " ")
	}
	// The chevron heads the thread's number, and folds from either.
	head := append([]look.Span{look.Chevron(open, false), look.Default(" ")}, threadName(t)...)
	if len(state.lead) > 0 {
		head = state.lead
	}
	spans := append(append([]look.Span{look.Muted("  ")}, head...), look.Muted(" "))
	spans = append(spans, look.State(t, d.isDraft(t.ReviewID))...)
	spans = append(spans, look.Muted("  "))
	spans = append(spans, where...)
	spans = append(spans, look.Muted(fmt.Sprintf("  %d comments  %s", len(t.Comments), first)))
	if open && len(state.trail) > 0 {
		spans = append(append(spans, look.Muted("  ")), state.trail...)
	}
	surf := look.Plain
	if state.cursor {
		surf = cursorSurface(state)
	}
	return r.th.DrawCut(surf, width, look.DimIf(t.State == store.ThreadResolved, spans...)...)
}

// renderThread draws a thread as a bordered box, and returns the line each comment starts on.
func (r *renderer) renderThread(d *changeData, t review.ThreadView, width int, state rowState, indent int, titled bool) ([]string, []int) {
	boxWidth := boxWidthIn(width, indent)
	inner := boxWidth - 4
	name := threadName(t)
	title := append(slices.Clip(name), look.Default(" "))
	title = append(title, look.State(t, d.isDraft(t.ReviewID))...)
	if loc := look.MarkSHA(commitMentions.Zone(t.Location.Commit), look.Location(t.Location)...); len(loc) > 0 && !t.Summary {
		title = append(append(title, look.Muted("  ")), loc...)
	}
	// The chevron that folds the thread heads its title, and its number, as it heads the thread
	// once folded.
	if len(state.lead) > 0 {
		title = append(slices.Clip(state.lead), title[len(name):]...)
	} else {
		title = append([]look.Span{look.Chevron(true, false), look.Default(" ")}, title...)
	}
	// A resolved thread recedes: its title and comment headers are dimmed.
	done := t.State == store.ThreadResolved
	var lines []string
	if titled {
		lines = append(lines, ansi.Truncate(r.th.Render(look.Plain, look.DimIf(done, title...)...), inner, ""))
	}
	// A thread a rewrite moved shows the lines it was made on, to read beside what replaced them.
	for i, l := range t.Was {
		label := "was: "
		if i > 0 {
			label = "     "
		}
		lines = append(lines, r.th.DrawCut(look.Plain, inner, look.Muted(label+termtext.Line(expandTabs(l)))))
	}
	var starts []int
	for i, c := range t.Comments {
		// The box's top border is the first output line.
		starts = append(starts, len(lines)+1)
		if titled || i > 0 || len(t.Was) > 0 {
			// Apart from the title, or the comment before.
			lines = append(lines, "")
		}
		lines = append(lines, r.th.Render(look.Plain, look.DimIf(done, commentHeader(d, c)...)...))
		lines = append(lines, r.renderMarkdown(c.Body, inner)...)
	}
	// The thread's controls sit in its bottom border, under its comments, where they take no line;
	// the last one's room for its key runs into the border's line. A box too narrow for them, as on
	// one side of a split diff, has them on lines of their own above the border instead, as many
	// as they need.
	controls := r.th.Render(look.Plain, look.OnBorder(state.cursor, state.controls...)...)
	dashes := boxWidth - 5 - ansi.StringWidth(controls)
	if len(state.controls) > 0 && dashes <= 0 {
		lines = append(lines, "")
		for _, l := range r.th.Wrap(inner, state.controls...) {
			lines = append(lines, ansi.Truncate(r.th.Render(look.Plain, l...), inner, ""))
		}
	} else {
		// Room between the last comment and the controls in the bottom border.
		lines = append(lines, "")
	}
	out := strings.Split(r.th.Box(boxWidth, state.cursor).Render(strings.Join(lines, "\n")), "\n")
	if n := len(out); len(state.controls) > 0 && dashes > 0 && n > 0 {
		out[n-1] = r.th.Render(look.Plain, look.Border("+- ", state.cursor)) + controls +
			r.th.Render(look.Plain, look.Border(strings.Repeat("-", dashes+1)+"+", state.cursor))
	}
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
