package tui

import (
	"fmt"
	"image/color"
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
)

// renderer draws rows. It caches what depends only on width.
type renderer struct {
	th       *theme
	hl       *highlighter
	markdown map[int]*glamour.TermRenderer
	// segments holds, after renderRow draws a thread, the line each of its comments starts on. The
	// first comment's segment starts at the row's first line, so it includes the excerpt and title.
	segments []int
}

func newRenderer(isDark bool, theme string) *renderer {
	return &renderer{th: newTheme(isDark, theme), hl: newHighlighter(isDark), markdown: map[int]*glamour.TermRenderer{}}
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
		return r.th.draw(surfActiveHunk, 1)
	}
	return " "
}

// cursorSurface is the cursor's surface, faint when the main pane does not have focus.
func cursorSurface(state rowState) surface {
	if state.dim {
		return surfCursorDim
	}
	return surfCursor
}

func (r *renderer) cursorBg(state rowState) color.Color {
	return r.th.surfaces[cursorSurface(state)].bg
}

// renderMarkdown renders a comment body, falling back to plain text.
func (r *renderer) renderMarkdown(body string, width int) []string {
	width = max(width, 10)
	body = termtext.Block(body)
	md, ok := r.markdown[width]
	if !ok {
		var err error
		md, err = glamour.NewTermRenderer(glamour.WithStyles(markdownStyle(r.th.isDark)), glamour.WithWordWrap(width))
		if err == nil {
			r.markdown[width] = md
		}
	}
	if md != nil {
		if out, err := md.Render(body); err == nil {
			return trimBlank(strings.Split(strings.TrimRight(out, "\n"), "\n"))
		}
	}
	return wrap(body, width)
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

func statusLetter(s git.FileStatus) string {
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

func fileLabel(f git.FileDiff) string {
	if f.OldPath != "" && f.NewPath != "" && f.OldPath != f.NewPath {
		return f.OldPath + " -> " + f.NewPath
	}
	return f.Path()
}

// renderRow draws one row at width. d supplies thread draft state.
func (r *renderer) renderRow(d *changeData, v view, rw row, width int, state rowState) []string {
	r.segments = nil
	switch rw.kind {
	case rowText:
		return rw.text(width)
	case rowCommit:
		return r.renderCommit(v.commit, width, state)
	case rowFile:
		f := v.files[rw.file]
		label := " " + statusLetter(f.Status) + " " + fileLabel(f)
		if f.Binary {
			label += "  (binary)"
		}
		if f.OldMode != "" && f.NewMode != "" && f.OldMode != f.NewMode {
			label += fmt.Sprintf("  (mode %s -> %s)", f.OldMode, f.NewMode)
		}
		surf := surfPlain
		if state.cursor {
			surf = cursorSurface(state)
		}
		spans := []span{sp(elPath, label)}
		if rw.reviewed {
			spans = append(spans, sp(elPath, "  "), sp(elReviewed, "reviewed", strong))
		}
		switch {
		case rw.collapsed:
			spans = append(spans, sp(elPath, "  "), sp(elMuted, "collapsed, "), sp(elKey, "enter"), sp(elKeyDesc, " expands"))
		case rw.reviewed:
			spans = append(spans, sp(elPath, "  "), sp(elKey, "enter"), sp(elKeyDesc, " collapses"))
		}
		return []string{r.th.draw(surf, width, spans...)}
	case rowHunk:
		h := rw.hunk
		label := fmt.Sprintf(" @@ -%d,%d +%d,%d @@ %s", h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Section)
		surf := surfHunk
		if state.cursor {
			surf = cursorSurface(state)
		}
		return []string{r.hunkMarker(state) + r.th.draw(surf, width-1, sp(elHunkRange, label))}
	case rowLine:
		f := v.files[rw.file]
		if rw.line != nil {
			return []string{r.hunkMarker(state) + r.renderUnified(f, *rw.line, width-1, state)}
		}
		return []string{r.hunkMarker(state) + r.renderSplit(f, rw.left, rw.right, width-1, state)}
	case rowThread:
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
		surf := surfPlain
		if state.cursor {
			surf = cursorSurface(state)
		}
		return []string{" " + r.th.draw(surf, width-1, sp(elMuted, label))}
	}
	return nil
}

// lineSurface is where a diff line sits: the cursor or selection on the cursor's side, or its
// added or deleted color.
func lineSurface(kind git.LineKind, state rowState, cursorSide bool) surface {
	switch {
	case state.cursor && cursorSide:
		return cursorSurface(state)
	case state.selected && cursorSide:
		return surfSelected
	case kind == git.LineAdded:
		return surfAdded
	case kind == git.LineDeleted:
		return surfDeleted
	}
	return surfPlain
}

func mark(kind git.LineKind) span {
	switch kind {
	case git.LineAdded:
		return sp(elMarkAdded, "+")
	case git.LineDeleted:
		return sp(elMarkDeleted, "-")
	}
	return sp(elDefault, " ")
}

// code is a line's text, highlighted.
func (r *renderer) code(f git.FileDiff, l git.Line) span {
	return styledSpan(func(bg color.Color) string { return r.hl.line(f.Path(), expandTabs(l.Text), bg) })
}

func num(n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}
	return fmt.Sprintf("%*d", width, n)
}

func (r *renderer) renderUnified(f git.FileDiff, l git.Line, width int, state rowState) string {
	gw := gutterWidth(f)
	return r.th.draw(lineSurface(l.Kind, state, true), width,
		sp(elGutter, num(l.OldLine, gw)+" "+num(l.NewLine, gw)+" "), mark(l.Kind), sp(elDefault, " "), r.code(f, l))
}

func (r *renderer) renderSplit(f git.FileDiff, left, right *git.Line, width int, state rowState) string {
	gw := gutterWidth(f)
	half := (width - 1) / 2
	cell := func(l *git.Line, side anchor.Side, w int) string {
		if l == nil {
			return r.th.draw(surfPlain, w)
		}
		n := l.NewLine
		if side == anchor.SideOld {
			n = l.OldLine
		}
		return r.th.draw(lineSurface(l.Kind, state, state.side == side), w,
			sp(elGutter, num(n, gw)+" "), mark(l.Kind), sp(elDefault, " "), r.code(f, *l))
	}
	return cell(left, anchor.SideOld, half) + r.th.render(surfPlain, sp(elBorder, "|")) + cell(right, anchor.SideNew, width-half-1)
}

func (r *renderer) renderCommit(c git.Commit, width int, state rowState) []string {
	surf := surfPlain
	if state.cursor {
		surf = cursorSurface(state)
	}
	return []string{r.th.draw(surf, width, sp(elSHA, "commit "+c.SHA, strong), sp(elDefault, "  "),
		sp(elAuthor, c.AuthorName), sp(elMuted, " <"+c.AuthorEmail+">  "+c.AuthorTime.Format("2006-01-02 15:04")))}
}

// stateSpans are a thread's state badge: its state and who set it, outdated, and draft.
func stateSpans(t review.ThreadView, draft bool) []span {
	var out []span
	switch t.State {
	case store.ThreadOpen:
		out = append(out, sp(elStateOpen, "open"))
	case store.ThreadAddressed:
		out = append(out, sp(elStateAddressed, "addressed"))
	case store.ThreadResolved:
		out = append(out, sp(elStateResolved, "resolved"))
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

// commentHeader is a comment's author and time, and whether it is a draft.
func commentHeader(d *changeData, c store.Comment) []span {
	out := []span{sp(elAuthor, c.Author.Name), sp(elMuted, "  "+c.CreatedAt.Local().Format("2006-01-02 15:04"))}
	if d.isDraft(c.ReviewID) {
		out = append(out, sp(elDraft, "  draft"))
	}
	return out
}

// renderExcerpt draws the diff a thread is about, under a line naming where it is.
func (r *renderer) renderExcerpt(e review.Excerpt, loc store.Location, width int) []string {
	where := append([]span{sp(elPath, " ")}, locationSpans(loc, elPath)...)
	if !e.Exact {
		where = append(where, sp(elPath, "  (outdated: as it was)"))
	}
	out := []string{r.th.draw(surfHeading, width, where...)}
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
		return append(lines, r.th.drawCut(surfPlain, width, sp(elMuted, line)))
	}
	r.segments = []int{0}
	for i, c := range t.Comments {
		if i > 0 {
			r.segments = append(r.segments, len(lines))
			lines = append(lines, "")
			lines = append(lines, r.th.draw(surfPlain, width, append([]span{sp(elDefault, "    ")}, commentHeader(d, c)...)...))
		}
		for _, l := range r.renderMarkdown(c.Body, width-6) {
			lines = append(lines, "    "+l)
		}
	}
	return lines
}

// renderCollapsedThread draws a folded thread on one line, as GitHub shows resolved threads.
func (r *renderer) renderCollapsedThread(d *changeData, t review.ThreadView, width int, state rowState) string {
	where := locationSpans(t.Location, elMuted)
	if t.Summary {
		where = []span{sp(elMuted, "summary")}
	}
	first := ""
	if len(t.Comments) > 0 {
		first = t.Comments[0].Author.Name + ": " + strings.Join(strings.Fields(t.Comments[0].Body), " ")
	}
	spans := []span{sp(elMuted, fmt.Sprintf("  > %s %s  ", t.Label(), spanText(stateSpans(t, d.isDraft(t.ReviewID)))))}
	spans = append(spans, where...)
	spans = append(spans, sp(elMuted, fmt.Sprintf("  %d comments  %s", len(t.Comments), first)))
	surf := surfPlain
	if state.cursor {
		surf = cursorSurface(state)
	}
	return r.th.drawCut(surf, width, spans...)
}

// renderThread draws a thread as a bordered box, and returns the line each comment starts on.
func (r *renderer) renderThread(d *changeData, t review.ThreadView, width int, cursor bool) ([]string, []int) {
	const indent = 2
	boxWidth := max(width-indent, 20)
	inner := boxWidth - 4
	title := []span{sp(elRef, t.Label()), sp(elDefault, " ")}
	if t.Summary {
		// A summary belongs to its review, so it is titled as the review's rather than numbered.
		title = []span{sp(elRef, "Summary"), sp(elMuted, " ("+t.Label()+")"), sp(elDefault, " ")}
	}
	title = append(title, stateSpans(t, d.isDraft(t.ReviewID))...)
	if loc := locationSpans(t.Location, elMuted); len(loc) > 0 && !t.Summary {
		title = append(append(title, sp(elMuted, "  ")), loc...)
	}
	lines := []string{ansi.Truncate(r.th.render(surfPlain, title...), inner, "")}
	var starts []int
	for _, c := range t.Comments {
		// The box's top border is the first output line.
		starts = append(starts, len(lines)+1)
		lines = append(lines, "", r.th.render(surfPlain, commentHeader(d, c)...))
		lines = append(lines, r.renderMarkdown(c.Body, inner)...)
	}
	out := strings.Split(r.th.box(boxWidth, cursor).Render(strings.Join(lines, "\n")), "\n")
	for i := range out {
		out[i] = strings.Repeat(" ", indent) + out[i]
	}
	if len(starts) == 0 {
		starts = []int{0}
	}
	return out, starts
}

// locationSpans name where a thread points: the commit's sha, and the path and lines as el.
func locationSpans(l store.Location, el element) []span {
	var out []span
	if l.Scope == store.ScopeChange {
		return nil
	}
	if l.Scope == store.ScopeCommit {
		out = append(out, sp(el, "commit "), sp(elSHA, shortSHA(l.Commit)))
	}
	if l.Path != "" {
		loc := l.Path
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

func shortSHA(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// isMessage reports whether a file is the commit message.
func isMessage(f git.FileDiff) bool { return f.NewPath == review.CommitMsgPath }
