package tui

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// fileMark is the mark for a file of a view, if the view's files can be marked.
func fileMark(v view, f int) (store.Mark, bool) {
	switch v.kind {
	case viewCommit:
		return review.CommitFileMark(v.commit.SHA, v.files[f]), true
	case viewDiff:
		return review.DiffFileMark(v.files[f]), true
	}
	return store.Mark{}, false
}

func (m *model) fileReviewed(v view, f int) bool {
	if v.kind == viewCommit {
		return m.data.marks.FileReviewed(v.commit.SHA, v.files[f])
	}
	mark, ok := fileMark(v, f)
	return ok && m.data.marks[mark]
}

// viewReviewed reports whether a commit, or every file of the whole diff, is reviewed.
func (m *model) viewReviewed(v view) bool {
	switch v.kind {
	case viewCommit:
		return m.data.marks.CommitReviewed(v.commit.SHA, v.files)
	case viewDiff:
		for f := range v.files {
			if !m.fileReviewed(v, f) {
				return false
			}
		}
		return len(v.files) > 0
	}
	return false
}

// collapsed reports whether a file shows only its header: reviewed files do, unless expanded.
func (m *model) collapsed(v view, f int) bool {
	expanded, toggled := m.expanded[v.key()+"\x00"+v.files[f].Path()]
	if toggled {
		return !expanded
	}
	return m.fileReviewed(v, f)
}

func (m *model) toggleCollapsed(v view, f int) {
	m.expanded[v.key()+"\x00"+v.files[f].Path()] = m.collapsed(v, f)
	m.rebuildMain()
	m.buildNav()
}

// cursorFile is the file the reviewed key applies to: the selected sidebar file, or the file under
// the main cursor.
func (m *model) cursorFile() int {
	if m.focus == focusFiles {
		if m.filesNav.pos < len(m.filesNav.items) {
			return m.filesNav.items[m.filesNav.pos].file
		}
		return -1
	}
	if m.main.cursor < len(m.main.rows) {
		return m.main.rows[m.main.cursor].file
	}
	return -1
}

// toggleReviewed marks or unmarks the focused file, or the commit when the commit list or the
// commit header has focus. Marking moves on to the next unreviewed file or commit.
func (m *model) toggleReviewed(wholeCommit bool) tea.Cmd {
	v := m.views[m.current]
	f := m.cursorFile()
	if v.kind == viewCommit && (wholeCommit || m.focus == focusViews || f < 0) {
		reviewed := !m.viewReviewed(v)
		m.applyCommitMark(v, reviewed)
		if reviewed {
			m.nextUnreviewedCommit()
		}
		svc, ctx, change, sha, files := m.svc, m.ctx, m.data.change, v.commit.SHA, v.files
		return func() tea.Msg {
			err := svc.SetCommitReviewed(ctx, change, sha, files, reviewed)
			return opMsg{status: reviewedStatus("commit "+shortSHA(sha), reviewed), err: err}
		}
	}
	if wholeCommit {
		m.setError(errors.New("not in a commit"))
		return nil
	}
	if f < 0 {
		m.setError(errors.New("move to a file to mark it reviewed"))
		return nil
	}
	mark, ok := fileMark(v, f)
	if !ok {
		m.setError(errors.New("files here cannot be marked reviewed"))
		return nil
	}
	reviewed := !m.fileReviewed(v, f)
	var cmds []func() error
	svc, ctx, change := m.svc, m.ctx, m.data.change
	if !reviewed && v.kind == viewCommit && m.data.marks[review.CommitMark(v.commit.SHA)] {
		// Unmarking one file of a reviewed commit: keep the other files marked.
		delete(m.data.marks, review.CommitMark(v.commit.SHA))
		for i := range v.files {
			if i != f {
				other := review.CommitFileMark(v.commit.SHA, v.files[i])
				m.data.marks[other] = true
				cmds = append(cmds, func() error { return svc.SetReviewed(ctx, change, other, true) })
			}
		}
		sha := v.commit.SHA
		cmds = append(cmds, func() error { return svc.SetReviewed(ctx, change, review.CommitMark(sha), false) })
	}
	if reviewed {
		m.data.marks[mark] = true
	} else {
		delete(m.data.marks, mark)
	}
	cmds = append(cmds, func() error { return svc.SetReviewed(ctx, change, mark, reviewed) })
	delete(m.expanded, v.key()+"\x00"+v.files[f].Path())
	m.rebuildMain()
	m.buildNav()
	if reviewed {
		m.nextUnreviewedFile(f)
	} else {
		m.focusFile(f)
	}
	label := v.files[f].Path()
	return func() tea.Msg {
		var errs []error
		for _, c := range cmds {
			errs = append(errs, c())
		}
		return opMsg{status: reviewedStatus(label, reviewed), err: errors.Join(errs...)}
	}
}

func reviewedStatus(what string, reviewed bool) string {
	if reviewed {
		return what + " marked reviewed"
	}
	return what + " marked unreviewed"
}

func (m *model) applyCommitMark(v view, reviewed bool) {
	mark := review.CommitMark(v.commit.SHA)
	if reviewed {
		m.data.marks[mark] = true
	} else {
		delete(m.data.marks, mark)
		for _, f := range v.files {
			delete(m.data.marks, review.CommitFileMark(v.commit.SHA, f))
		}
	}
	for _, f := range v.files {
		delete(m.expanded, v.key()+"\x00"+f.Path())
	}
	m.rebuildMain()
	m.buildNav()
}

// focusFile puts the main cursor on a file's header and selects it in the sidebar.
func (m *model) focusFile(f int) {
	for i, r := range m.main.rows {
		if r.kind == rowFile && r.file == f {
			m.main.jump(i)
			break
		}
	}
	if f < len(m.filesNav.items) {
		m.filesNav.pos = f
	}
}

// nextUnreviewedFile moves to the first unreviewed file after f, wrapping around. When the view has
// none left and is a commit, it moves on to the next unreviewed commit.
func (m *model) nextUnreviewedFile(f int) {
	v := m.views[m.current]
	n := len(v.files)
	for i := 1; i < n; i++ {
		next := (f + i) % n
		if !m.fileReviewed(v, next) {
			m.focusFile(next)
			return
		}
	}
	if v.kind == viewCommit {
		m.nextUnreviewedCommit()
		return
	}
	m.focusFile(f)
	m.setStatus("every file is reviewed")
}

// nextUnreviewedCommit opens the next commit not yet reviewed, wrapping around, at its first
// unreviewed file.
func (m *model) nextUnreviewedCommit() {
	n := len(m.views)
	for i := 1; i < n; i++ {
		vi := (m.current + i) % n
		v := m.views[vi]
		if v.kind != viewCommit || m.viewReviewed(v) {
			continue
		}
		m.viewsNav.pos = vi
		m.openView()
		for f := range v.files {
			if !m.fileReviewed(v, f) {
				m.focusFile(f)
				break
			}
		}
		return
	}
	m.setStatus("every commit is reviewed; S to submit")
}

// reviewProgress counts reviewed commits.
func (m *model) reviewProgress() string {
	done, total := 0, 0
	for _, v := range m.views {
		if v.kind == viewCommit {
			total++
			if m.viewReviewed(v) {
				done++
			}
		}
	}
	return fmt.Sprintf("%d/%d commits reviewed", done, total)
}
