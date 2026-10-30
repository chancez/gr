package tui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"charm.land/glamour/v2"
	glamouransi "charm.land/glamour/v2/ansi"
	glamourstyles "charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// renderer draws rows. It caches what depends only on width.
type renderer struct {
	st       styles
	hl       *highlighter
	markdown map[int]*glamour.TermRenderer
}

func newRenderer(isDark bool) *renderer {
	return &renderer{st: newStyles(isDark), hl: newHighlighter(isDark), markdown: map[int]*glamour.TermRenderer{}}
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
		return lipgloss.NewStyle().Background(r.st.accent).Render(" ")
	}
	return " "
}

func (r *renderer) cursorBg(state rowState) color.Color {
	if state.dim {
		return r.st.dimCursorBg
	}
	return r.st.cursorBg
}

// renderMarkdown renders a comment body, falling back to plain text.
func (r *renderer) renderMarkdown(body string, width int) []string {
	width = max(width, 10)
	md, ok := r.markdown[width]
	if !ok {
		var err error
		md, err = glamour.NewTermRenderer(glamour.WithStyles(markdownStyle(r.st.isDark)), glamour.WithWordWrap(width))
		if err == nil {
			r.markdown[width] = md
		}
	}
	if md != nil {
		if out, err := md.Render(body); err == nil {
			return trimBlank(strings.Split(strings.TrimRight(out, "\n"), "\n"))
		}
	}
	return strings.Split(lipgloss.NewStyle().Width(width).Render(body), "\n")
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

// fit truncates or pads a styled string to exactly width cells, padding with bg.
func fit(s string, width int, bg color.Color) string {
	if width <= 0 {
		return ""
	}
	s = ansi.Truncate(s, width, "")
	pad := width - ansi.StringWidth(s)
	if pad > 0 {
		style := lipgloss.NewStyle()
		if bg != nil {
			style = style.Background(bg)
		}
		s += style.Render(strings.Repeat(" ", pad))
	}
	return s
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
	st := r.st
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
		if rw.reviewed {
			label += "  [reviewed]"
		}
		if rw.collapsed {
			label += "  (collapsed, enter expands)"
		} else if rw.reviewed {
			label += "  (enter collapses)"
		}
		style := st.fileHeader
		if state.cursor {
			style = style.Background(r.cursorBg(state))
		}
		return []string{style.Render(fit(label, width, nil))}
	case rowHunk:
		h := rw.hunk
		label := fmt.Sprintf(" @@ -%d,%d +%d,%d @@ %s", h.OldStart, h.OldLines, h.NewStart, h.NewLines, h.Section)
		style := st.hunkHeader
		if state.cursor {
			style = style.Background(r.cursorBg(state))
		}
		return []string{r.hunkMarker(state) + style.Render(fit(label, width-1, nil))}
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
		lines := r.renderThread(d, *rw.thread, width, state.cursor)
		if rw.excerpt != nil {
			lines = append(r.renderExcerpt(*rw.excerpt, rw.thread.Location, width), lines...)
		}
		return lines
	case rowGap:
		label := "   " + gapLabel(*rw.gap) + "  (zo or enter to expand)"
		style := st.muted
		if state.cursor {
			style = style.Background(r.cursorBg(state))
		}
		return []string{" " + style.Render(fit(label, width-1, nil))}
	}
	return nil
}

func (r *renderer) lineBg(kind git.LineKind, state rowState, cursorSide bool) color.Color {
	switch {
	case state.cursor && cursorSide:
		return r.cursorBg(state)
	case state.selected && cursorSide:
		return r.st.selectedBg
	case kind == git.LineAdded:
		return r.st.addedBg
	case kind == git.LineDeleted:
		return r.st.deletedBg
	}
	return nil
}

func (r *renderer) mark(kind git.LineKind, bg color.Color) string {
	switch kind {
	case git.LineAdded:
		return r.st.addedMark.Background(bg).Render("+")
	case git.LineDeleted:
		return r.st.deletedMark.Background(bg).Render("-")
	}
	return lipgloss.NewStyle().Background(bg).Render(" ")
}

func num(n, width int) string {
	if n == 0 {
		return strings.Repeat(" ", width)
	}
	return fmt.Sprintf("%*d", width, n)
}

func (r *renderer) renderUnified(f git.FileDiff, l git.Line, width int, state rowState) string {
	gw := gutterWidth(f)
	bg := r.lineBg(l.Kind, state, true)
	gutter := r.st.gutter
	if bg != nil {
		gutter = gutter.Background(bg)
	}
	prefix := gutter.Render(num(l.OldLine, gw)+" "+num(l.NewLine, gw)+" ") + r.mark(l.Kind, bg) + lipgloss.NewStyle().Background(bg).Render(" ")
	text := r.hl.line(f.Path(), expandTabs(l.Text), bg)
	return fit(prefix+text, width, bg)
}

func (r *renderer) renderSplit(f git.FileDiff, left, right *git.Line, width int, state rowState) string {
	gw := gutterWidth(f)
	half := (width - 1) / 2
	cell := func(l *git.Line, side anchor.Side, w int) string {
		if l == nil {
			return fit("", w, nil)
		}
		n := l.NewLine
		if side == anchor.SideOld {
			n = l.OldLine
		}
		bg := r.lineBg(l.Kind, state, state.side == side)
		gutter := r.st.gutter
		if bg != nil {
			gutter = gutter.Background(bg)
		}
		prefix := gutter.Render(num(n, gw)+" ") + r.mark(l.Kind, bg) + lipgloss.NewStyle().Background(bg).Render(" ")
		return fit(prefix+r.hl.line(f.Path(), expandTabs(l.Text), bg), w, bg)
	}
	return cell(left, anchor.SideOld, half) + r.st.paneBorder.Render("|") + cell(right, anchor.SideNew, width-half-1)
}

func (r *renderer) renderCommit(c git.Commit, width int, state rowState) []string {
	st := r.st
	head := st.commitHeader.Render("commit "+c.SHA) + "  " + st.muted.Render(c.AuthorName+" <"+c.AuthorEmail+">  "+c.AuthorTime.Format("2006-01-02 15:04"))
	lines := []string{fit(head, width, nil)}
	if state.cursor {
		bg := r.cursorBg(state)
		lines[0] = lipgloss.NewStyle().Background(bg).Render(fit(head, width, bg))
	}
	return lines
}

func stateBadge(st styles, t review.ThreadView, draft bool) string {
	var parts []string
	switch t.State {
	case store.ThreadOpen:
		parts = append(parts, st.badgeOpen.Render("open"))
	case store.ThreadAddressed:
		parts = append(parts, st.badgeWarn.Render("addressed"))
	case store.ThreadResolved:
		parts = append(parts, st.badgeDone.Render("resolved"))
	}
	if t.State != store.ThreadOpen && t.StateBy.Name != "" {
		parts[0] += st.muted.Render(" by " + t.StateBy.Name)
	}
	if t.Outdated {
		parts = append(parts, st.badgeWarn.Render("outdated"))
	}
	if draft {
		parts = append(parts, st.badgeWarn.Render("draft"))
	}
	return strings.Join(parts, st.muted.Render(", "))
}

// renderExcerpt draws the diff a thread is about, under a line naming where it is.
func (r *renderer) renderExcerpt(e review.Excerpt, loc store.Location, width int) []string {
	st := r.st
	where := st.fileHeader.Render(fit(" "+describeLocation(loc), width, nil))
	if !e.Exact {
		where = st.fileHeader.Render(fit(" "+describeLocation(loc)+"  (outdated: as it was)", width, nil))
	}
	out := []string{where}
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

// renderCollapsedThread draws a folded thread on one line, as GitHub shows resolved threads.
func (r *renderer) renderCollapsedThread(d *changeData, t review.ThreadView, width int, state rowState) string {
	st := r.st
	where := describeLocation(t.Location)
	if t.Summary {
		where = "review summary"
	}
	first := ""
	if len(t.Comments) > 0 {
		first = t.Comments[0].Author.Name + ": " + strings.Join(strings.Fields(t.Comments[0].Body), " ")
	}
	text := fmt.Sprintf("  > #%d %s  %s  %d comments  %s", t.Seq, ansi.Strip(stateBadge(st, t, d.isDraft(t.ReviewID))),
		where, len(t.Comments), first)
	style := st.muted
	if state.cursor {
		style = style.Background(r.cursorBg(state))
	}
	return style.Render(fit(ansi.Truncate(text, width, "..."), width, nil))
}

// renderThread draws a thread as a bordered box, indented under the line it is on.
func (r *renderer) renderThread(d *changeData, t review.ThreadView, width int, cursor bool) []string {
	st := r.st
	const indent = 2
	boxWidth := max(width-indent, 20)
	inner := boxWidth - 4
	title := st.threadTitle.Render(fmt.Sprintf("#%d", t.Seq)) + " " + stateBadge(st, t, d.isDraft(t.ReviewID))
	if t.Summary {
		title += st.muted.Render("  review summary")
	} else if loc := describeLocation(t.Location); loc != "" {
		title += st.muted.Render("  " + loc)
	}
	lines := []string{ansi.Truncate(title, inner, "")}
	for _, c := range t.Comments {
		header := st.author.Render(c.Author.Name) + st.muted.Render("  "+c.CreatedAt.Local().Format("2006-01-02 15:04"))
		if d.isDraft(c.ReviewID) {
			header += st.badgeWarn.Render("  draft")
		}
		lines = append(lines, "", header)
		lines = append(lines, r.renderMarkdown(c.Body, inner)...)
	}
	box := st.threadBorder.Width(boxWidth)
	if cursor {
		box = box.BorderForeground(st.accent)
	}
	out := strings.Split(box.Render(strings.Join(lines, "\n")), "\n")
	for i := range out {
		out[i] = strings.Repeat(" ", indent) + out[i]
	}
	return out
}

// describeLocation is a short label for where a thread points.
func describeLocation(l store.Location) string {
	var parts []string
	switch l.Scope {
	case store.ScopeChange:
		return ""
	case store.ScopeCommit:
		parts = append(parts, "commit "+shortSHA(l.Commit))
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
		parts = append(parts, loc)
	}
	return strings.Join(parts, " ")
}

func shortSHA(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

// isMessage reports whether a file is the commit message.
func isMessage(f git.FileDiff) bool { return f.NewPath == review.CommitMsgPath }
