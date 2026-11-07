package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/look"
	"github.com/charmbracelet/x/ansi"
	"github.com/guygrigsby/vimbubble/v2"
)

type composeKind int

const (
	composeThread composeKind = iota
	composeReply
	composeEdit
	composeSubmit
)

type composer struct {
	kind  composeKind
	title string
	// warn shows the title as a warning.
	warn      bool
	area      textarea.Model
	thread    review.NewThread
	threadID  string
	commentID string
	verdict   store.Verdict
	// initial is the text the box opened with, so closing it unchanged asks nothing.
	initial string
	// closing is set while esc asks what to do with unsaved text.
	closing bool
	// at is the key of the row the box opens under, to find it again when the rows are rebuilt.
	at string
	// vim, when set, edits the text with vim's modes.
	vim *vimbubble.Modal
}

// commentOnRow opens the comment box on row i of the main pane: its line, on side in split mode, or
// file, or, when whole, its whole file or commit.
func (m *model) commentOnRow(i int, side anchor.Side, whole bool) tea.Cmd {
	v := m.views[m.current]
	if v.readOnly() {
		m.setError(errors.New("the interdiff is read-only; comment on the files or commits"))
		return nil
	}
	nt, title, err := m.newThreadAt(v, i, side, whole)
	if err != nil {
		m.setError(err)
		return nil
	}
	return m.commentOn(nt, title)
}

// commentOn opens the comment box for a new thread nt. Commenting where your draft already is
// continues it, rather than starting a second one.
func (m *model) commentOn(nt review.NewThread, title string) tea.Cmd {
	if t, c, ok := m.draftAt(nt); ok {
		return m.openComposer(composer{kind: composeEdit, threadID: t.ID, commentID: c.ID,
			title: fmt.Sprintf("Edit your draft %s", t.Label())}, c.Body)
	}
	return m.openComposer(composer{kind: composeThread, title: title, thread: nt}, "")
}

// commentOnChange opens the comment box on the change as a whole, without changing what is shown.
func (m *model) commentOnChange() tea.Cmd {
	return m.commentOn(review.NewThread{Scope: store.ScopeChange}, "Comment on the change")
}

// commentOnCommit opens the comment box on the commit v shows.
func (m *model) commentOnCommit(v view) tea.Cmd {
	return m.commentOn(review.NewThread{Scope: store.ScopeCommit, Commit: v.commit.SHA},
		"Comment on commit "+shortSHA(v.commit.SHA))
}

// commentOnFile opens the comment box on file f of view v as a whole, without changing what is
// shown.
func (m *model) commentOnFile(v view, f int) tea.Cmd {
	if v.readOnly() {
		m.setError(errors.New("the interdiff is read-only; comment on the files or commits"))
		return nil
	}
	file := v.files[f]
	nt := review.NewThread{Scope: store.ScopeDiff, Path: file.NewPath, Side: anchor.SideNew}
	where := "the diff"
	if v.kind == viewCommit {
		nt.Scope, nt.Commit = store.ScopeCommit, v.commit.SHA
		where = "commit " + shortSHA(v.commit.SHA)
	}
	if file.NewPath == "" {
		nt.Path, nt.Side = file.OldPath, anchor.SideOld
	}
	return m.commentOn(nt, fmt.Sprintf("Comment on %s in %s", nt.Path, where))
}

// draftAt finds your draft thread exactly where nt would start one, and its comment. Comments on
// the change as a whole are not matched, since several are normal.
func (m *model) draftAt(nt review.NewThread) (review.ThreadView, store.Comment, bool) {
	if nt.Scope == store.ScopeChange {
		return review.ThreadView{}, store.Comment{}, false
	}
	end := nt.EndLine
	if end == 0 {
		end = nt.StartLine
	}
	sideOf := func(s anchor.Side) anchor.Side {
		if s == "" {
			return anchor.SideNew
		}
		return s
	}
	for _, t := range m.data.threads {
		l := t.Location
		if t.Summary || !m.data.isDraft(t.ReviewID) || len(t.Comments) != 1 || t.Comments[0].Author != m.svc.Author() {
			continue
		}
		if l.Scope == nt.Scope && l.Commit == nt.Commit && l.Path == nt.Path && sideOf(l.Side) == sideOf(nt.Side) &&
			l.StartLine == nt.StartLine && l.EndLine == end {
			return t, t.Comments[0], true
		}
	}
	return review.ThreadView{}, store.Comment{}, false
}

// newThreadAt is where a new thread on row i would be: its line, or the selection when i is the
// cursor's, its file when whole, or the commit.
func (m *model) newThreadAt(v view, i int, side anchor.Side, whole bool) (review.NewThread, string, error) {
	if v.kind == viewOverview {
		return review.NewThread{Scope: store.ScopeChange}, "Comment on the change", nil
	}
	nt := review.NewThread{Scope: store.ScopeDiff}
	where := "the diff"
	if v.kind == viewCommit {
		nt.Scope, nt.Commit = store.ScopeCommit, v.commit.SHA
		where = "commit " + shortSHA(v.commit.SHA)
	}
	p := &m.main
	if i < 0 || i >= len(p.rows) {
		return nt, "", errors.New("nothing to comment on")
	}
	cur := p.rows[i]
	if cur.kind == rowCommit || (whole && cur.file < 0) {
		if v.kind != viewCommit {
			return nt, "", errors.New("move to a file to comment")
		}
		return nt, "Comment on " + where, nil
	}
	if cur.file < 0 {
		return nt, "", errors.New("move to a file or line to comment")
	}
	if cur.gap != nil {
		return nt, "", errors.New("unchanged lines outside the diff cannot be commented on yet")
	}
	f := v.files[cur.file]
	if cur.kind == rowFile || cur.kind == rowHunk || cur.kind == rowThread || whole {
		nt.Path, nt.Side = f.NewPath, anchor.SideNew
		if f.NewPath == "" {
			nt.Path, nt.Side = f.OldPath, anchor.SideOld
		}
		return nt, fmt.Sprintf("Comment on %s in %s", nt.Path, where), nil
	}

	// Lines: the cursor line decides the side, and the selection gives the range on that side.
	side = lineSide(cur, side)
	nt.Side, nt.Path = side, f.NewPath
	if side == anchor.SideOld {
		nt.Path = f.OldPath
	}
	selStart, selEnd := i, i
	if i == p.cursor {
		selStart, selEnd = p.selection()
	}
	for i := selStart; i <= selEnd; i++ {
		r := p.rows[i]
		if r.kind != rowLine {
			continue
		}
		if r.file != cur.file {
			return nt, "", errors.New("a selection must stay within one file")
		}
		n := lineOn(r, side)
		if n == 0 {
			continue
		}
		if nt.StartLine == 0 || n < nt.StartLine {
			nt.StartLine = n
		}
		nt.EndLine = max(nt.EndLine, n)
	}
	if nt.StartLine == 0 {
		return nt, "", errors.New("no lines on that side")
	}
	lines := fmt.Sprintf("%d", nt.StartLine)
	if nt.EndLine != nt.StartLine {
		lines += fmt.Sprintf("-%d", nt.EndLine)
	}
	sideLabel := ""
	if side == anchor.SideOld {
		sideLabel = " (old)"
	}
	return nt, fmt.Sprintf("Comment on %s:%s%s in %s", nt.Path, lines, sideLabel, where), nil
}

// lineSide is the side a line row is commented on: deletions are old, everything else new. In split
// mode it is side, falling back to whichever side has a line.
func lineSide(r row, side anchor.Side) anchor.Side {
	if r.line != nil {
		if r.line.Kind == git.LineDeleted {
			return anchor.SideOld
		}
		return anchor.SideNew
	}
	if side == anchor.SideOld && r.left != nil || r.right == nil {
		return anchor.SideOld
	}
	return anchor.SideNew
}

func lineOn(r row, side anchor.Side) int {
	old, new := lineNumbers(r)
	if r.line != nil {
		if side == anchor.SideOld && r.line.Kind == git.LineDeleted {
			return old
		}
		if side == anchor.SideNew && r.line.Kind != git.LineDeleted {
			return new
		}
		return 0
	}
	if side == anchor.SideOld {
		return old
	}
	return new
}

func (m *model) openComposer(c composer, initial string) tea.Cmd {
	c.area = textarea.New()
	c.area.ShowLineNumbers = false
	c.area.Placeholder = "Markdown. ctrl+s to save, ctrl+e for $EDITOR, esc to close."
	c.area.SetStyles(m.composeStyles())
	c.initial = initial
	c.area.SetValue(initial)
	c.area.SetHeight(composeHeight)
	top, at := m.composeAnchor(c)
	if at < len(m.main.rows) {
		c.at = m.main.rows[at].key(m.views[m.current].files)
	}
	m.compose = c
	if m.vim {
		// It edits the box's own text area, so it is made once the box is in place.
		m.compose.vim = vimbubble.New(&m.compose.area)
		m.compose.vim.SetEnabled(true)
		m.compose.vim.SetMode(vimbubble.Insert)
		// The terminal's own cursor, which can change shape with the mode, as vim's does; the text
		// area's drawn one is always a block.
		styles := m.compose.area.Styles()
		styles.Cursor.Blink = false
		m.compose.area.SetStyles(styles)
		m.compose.area.SetVirtualCursor(false)
	}
	m.mode = modeCompose
	// The box opens under what it comments on, as GitHub's, so the lines above stay put.
	m.main.attach, m.main.attached = at, m.composeLines
	m.relayout(false)
	m.main.layout(m.renderMainRow)
	m.main.showAttached(top)
	return m.compose.area.Focus()
}

// composeIndent is how far the comment box is indented from the main pane's edge: as far as the
// thread it starts or replies to will be, so it opens where that thread shows.
func (m *model) composeIndent() int {
	p := &m.main
	if p.attach < 0 || p.attach >= len(p.rows) {
		return 2
	}
	v, r := m.views[m.current], p.rows[p.attach]
	switch {
	case r.kind == rowThread:
		return threadIndent(v, r, m.split, p.width)
	case m.compose.kind == composeThread && m.compose.thread.StartLine > 0:
		// The thread it starts, on the lines it hangs under.
		return codeIndent(v, r, m.split, sideOr(m.compose.thread.Side), p.width)
	}
	return 2
}

// composeAnchor is the row the comment box opens under, and the first row of what it comments on:
// a thread's row for a reply or an edit, the last line commented on with the threads already
// under it, a file's header, or the commit's; otherwise the cursor's.
func (m *model) composeAnchor(c composer) (top, at int) {
	p := &m.main
	files := m.views[m.current].files
	path := func(r row) (string, string) {
		if r.file < 0 || r.file >= len(files) {
			return "", ""
		}
		return files[r.file].NewPath, files[r.file].OldPath
	}
	switch {
	case c.kind == composeReply || c.kind == composeEdit:
		for i, r := range p.rows {
			if (r.kind == rowThread || r.kind == rowReview) && r.thread != nil && r.thread.ID == c.threadID {
				return i, i
			}
		}
	case c.kind == composeThread && c.thread.StartLine > 0:
		nt := c.thread
		end := max(nt.EndLine, nt.StartLine)
		top, at = -1, -1
		for i, r := range p.rows {
			newPath, oldPath := path(r)
			if r.kind != rowLine || (newPath != nt.Path && oldPath != nt.Path) {
				continue
			}
			if n := lineOn(r, sideOr(nt.Side)); n >= nt.StartLine && n <= end {
				if top < 0 {
					top = i
				}
				at = i
			}
		}
		if at >= 0 {
			for at+1 < len(p.rows) && p.rows[at+1].kind == rowThread {
				at++
			}
			return top, at
		}
	case c.kind == composeThread && c.thread.Path != "":
		for i, r := range p.rows {
			if newPath, oldPath := path(r); r.kind == rowFile && (newPath == c.thread.Path || oldPath == c.thread.Path) {
				return i, i
			}
		}
	case c.kind == composeThread && c.thread.Scope == store.ScopeCommit:
		for i, r := range p.rows {
			if r.kind == rowCommit {
				return i, i
			}
		}
	}
	return p.cursor, p.cursor
}

// sideOr is s, or the new side when it is unset.
func sideOr(s anchor.Side) anchor.Side {
	if s == "" {
		return anchor.SideNew
	}
	return s
}

// composeCursor is the terminal's cursor in the comment box with vim, where the text area's
// cursor is on screen: a bar in insert mode and a block otherwise, steady. It is nil without vim,
// whose text area draws its own, and when the box's text is scrolled out of view.
func (m *model) composeCursor() *tea.Cursor {
	if m.mode != modeCompose || m.compose.vim == nil {
		return nil
	}
	c := m.compose.area.Cursor()
	p := &m.main
	if c == nil || !p.attaching(p.attach) {
		return nil
	}
	nw, _, _ := m.layoutSizes()
	left := 0
	if nw > 0 {
		left = nw + 1
	}
	// The box's top border and title are above its text, inside the indent, border, and padding.
	end := p.starts[p.attach] + p.rowHeight(p.attach)
	top := end - len(p.attached(p.width)) + 2
	y := m.mainTop() + top - p.offset + c.Y
	if y < m.mainTop() || y >= m.mainTop()+p.height {
		return nil
	}
	c.X += left + m.composeIndent() + 2
	if p.reader {
		// The overview draws its rows after a margin, the selected item's marker's.
		c.X++
	}
	c.Y = y
	c.Shape = tea.CursorBlock
	if m.compose.vim.Mode() == vimbubble.Insert {
		c.Shape = tea.CursorBar
	}
	return c
}

// composeLines draws the comment box, width wide, as it hangs under its row.
func (m *model) composeLines(width int) []string {
	th := m.r.th
	indent := m.composeIndent()
	boxWidth := boxWidthIn(width, indent)
	inner := boxWidth - 4
	title := []look.Span{look.Heading(m.compose.title)}
	if m.compose.warn {
		title = []look.Span{look.Warning(m.compose.title)}
	}
	if m.compose.vim != nil {
		title = append(title, look.Default("  "), look.Mode(m.compose.vim.ModeLabel()))
	}
	buttons := th.Render(look.Plain, m.controls("box", modeCompose, theComposer.Target(composeSubject{}),
		button(actComposeSave, look.GlyphSave, "Save"),
		button(actComposeClose, look.GlyphCancel, "Cancel"),
		button(actComposeEditor, look.GlyphEditor, "$EDITOR"))...)
	m.compose.area.SetWidth(inner)
	box := th.Box(boxWidth, true).Render(th.DrawCut(look.Plain, inner, title...) + "\n" + m.compose.area.View() + "\n" +
		ansi.Truncate(buttons, inner, ""))
	lines := strings.Split(box, "\n")
	for i := range lines {
		lines[i] = strings.Repeat(" ", indent) + lines[i]
	}
	return lines
}

// composeStyles colors the comment editor from the palette, so text and hints are readable on any
// background.
func (m *model) composeStyles() textarea.Styles {
	return m.r.th.EditorStyles()
}

func (m *model) closeComposer() {
	m.mode = modeNormal
	m.compose = composer{}
	m.main.attach, m.main.attached = -1, nil
	m.main.layout(m.renderMainRow)
	m.relayout(false)
}

func (m *model) composeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	box := theComposer.Target(composeSubject{})
	if m.compose.closing {
		// The unsaved comment prompt: any key but its own goes back to editing.
		m.compose.closing = false
		c, _ := m.runOn(msg, theClosing.Target(composeSubject{}))
		return m, c
	}
	vim := m.compose.vim
	// With vim, esc leaves insert and visual mode first; in normal mode it is the box's, as without.
	if vim != nil && msg.Key().Code == tea.KeyEscape && vim.Mode() != vimbubble.Normal {
		_, c := vim.Update(msg)
		return m, c
	}
	if c, ok := m.runOn(msg, box); ok {
		return m, c
	}
	if vim == nil || vim.Mode() == vimbubble.Insert {
		msg = unshifted(msg)
	}
	if vim != nil {
		if consumed, c := vim.Update(msg); consumed {
			return m, c
		}
	}
	var cmd tea.Cmd
	m.compose.area, cmd = m.compose.area.Update(msg)
	return m, cmd
}

// unshifted is a key typed with shift held where shift means nothing to the text area, as
// shift+enter, without it: the text area binds only the plain keys, so these did nothing. Shift
// with tab is left, as it means something of its own.
func unshifted(msg tea.KeyPressMsg) tea.KeyPressMsg {
	k := msg.Key()
	if k.Mod != tea.ModShift {
		return msg
	}
	switch k.Code {
	case tea.KeyEnter, tea.KeyBackspace, tea.KeyDelete, tea.KeyLeft, tea.KeyRight, tea.KeyUp, tea.KeyDown,
		tea.KeyHome, tea.KeyEnd, tea.KeyPgUp, tea.KeyPgDown:
		k.Mod = 0
		return tea.KeyPressMsg(k)
	}
	return msg
}

// closeOrAsk closes the comment box, or asks first when it has unsaved text, so a reflexive esc
// cannot lose a comment.
func (m *model) closeOrAsk() {
	if v := m.compose.area.Value(); strings.TrimSpace(v) == "" || v == m.compose.initial {
		m.closeComposer()
		return
	}
	m.compose.closing = true
}

func (m *model) saveCompose() (tea.Model, tea.Cmd) {
	c := m.compose
	body := strings.TrimSpace(c.area.Value())
	if body == "" && c.kind != composeSubmit {
		m.setError(errors.New("comment is empty"))
		return m, nil
	}
	svc, ctx, change := m.svc, m.ctx, m.data.change
	m.closeComposer()
	m.main.anchor = -1
	return m, func() tea.Msg {
		switch c.kind {
		case composeThread:
			c.thread.Body = body
			t, err := svc.StartThread(ctx, change, c.thread)
			return opMsg{status: fmt.Sprintf("draft thread #%d added; S to submit", t.Seq), err: err}
		case composeReply:
			_, err := svc.Reply(ctx, change, c.threadID, body, false)
			return opMsg{status: "replied", err: err}
		case composeEdit:
			return opMsg{status: "comment updated", err: svc.EditComment(ctx, c.commentID, body)}
		case composeSubmit:
			r, err := svc.Submit(ctx, change, c.verdict, body)
			return opMsg{status: fmt.Sprintf("submitted review #%d: %s", r.Seq, r.Verdict), err: err}
		}
		return nil
	}
}

// verdictKey takes the verdict: a, r, or c open the summary editor, and A, R, or C submit straight
// away with no new summary, keeping any already in the draft.
func (m *model) verdictKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	c, ok := m.runOn(msg, theVerdict.Target(verdictSubject{}))
	if !ok {
		m.mode = modeNormal
	}
	return m, c
}

// submitWith submits the draft review with verdict: now, without a new summary, keeping any
// already in the draft, or after the comment box asks for one.
func (m *model) submitWith(verdict store.Verdict, now bool) tea.Cmd {
	if now {
		m.mode = modeNormal
		svc, ctx, change := m.svc, m.ctx, m.data.change
		return func() tea.Msg {
			r, err := svc.Submit(ctx, change, verdict, "")
			return opMsg{status: fmt.Sprintf("submitted review #%d: %s", r.Seq, r.Verdict), err: err}
		}
	}
	initial := ""
	if d, ok := m.data.draft(); ok {
		initial = d.Body
	}
	return m.openComposer(composer{kind: composeSubmit, verdict: verdict, title: "Submit review: " + string(verdict) + " (summary is optional)"}, initial)
}

// editorCmd suspends the TUI and edits text in $VISUAL or $EDITOR.
func editorCmd(text string) tea.Cmd {
	editor := editorProgram()
	tmp, err := os.CreateTemp("", "gr-*.md")
	if err != nil {
		return func() tea.Msg { return editorMsg{err: err} }
	}
	_, werr := tmp.WriteString(text)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return func() tea.Msg { return editorMsg{err: err} }
	}
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", tmp.Name())
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(tmp.Name())
		if err != nil {
			return editorMsg{err: err}
		}
		b, err := os.ReadFile(tmp.Name())
		return editorMsg{text: strings.TrimRight(string(b), "\n"), err: err}
	})
}
