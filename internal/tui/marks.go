package tui

import (
	"errors"
	"fmt"
	"slices"

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
		// The message is reviewed with the commit, not as a file of its own.
		return m.data.marks.CommitReviewed(v.commit.SHA, slices.DeleteFunc(slices.Clone(v.files), isMessage))
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

// toggleCommitReviewed marks or unmarks every file of a commit's view reviewed. Marking the commit
// shown advances; marking one named elsewhere, as by a mention, stays put.
func (m *model) toggleCommitReviewed(v view) tea.Cmd {
	reviewed := !m.viewReviewed(v)
	m.applyCommitMark(v, reviewed)
	note := ""
	if reviewed && m.autoAdvance && v.key() == m.views[m.current].key() {
		note = m.advance(m.nextUnreviewedCommit)
	}
	svc, ctx, change, sha, files := m.svc, m.ctx, m.data.change, v.commit.SHA, v.files
	return func() tea.Msg {
		err := svc.SetCommitReviewed(ctx, change, sha, files, reviewed)
		return opMsg{status: reviewedStatus("commit "+shortSHA(sha), reviewed) + note, err: err}
	}
}

// toggleFileReviewed marks or unmarks file f of view v reviewed.
func (m *model) toggleFileReviewed(v view, f int) tea.Cmd {
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
	commit := ""
	if v.kind == viewCommit {
		commit = v.commit.SHA
	}
	if !reviewed && commit != "" && m.data.marks[review.CommitMark(commit)] {
		// Unmarking one file of a reviewed commit keeps the other files marked.
		delete(m.data.marks, review.CommitMark(commit))
		for i := range v.files {
			if i != f {
				m.data.marks[review.CommitFileMark(commit, v.files[i])] = true
			}
		}
	}
	if reviewed {
		m.data.marks[mark] = true
	} else {
		delete(m.data.marks, mark)
	}
	delete(m.expanded, v.key()+"\x00"+v.files[f].Path())
	m.rebuildMain()
	m.buildNav()
	m.focusFile(f)
	note := ""
	if reviewed && m.autoAdvance {
		note = m.advance(func() { m.nextUnreviewedFile(f) })
	}
	svc, ctx, change, files, file := m.svc, m.ctx, m.data.change, v.files, v.files[f]
	return func() tea.Msg {
		err := svc.SetFileReviewed(ctx, change, commit, files, file, reviewed)
		return opMsg{status: reviewedStatus(file.Path(), reviewed) + note, err: err}
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
	m.filesNav.show(m.current, f)
}

// advance runs an auto-advance move as a jump, so ctrl+o returns to what was marked, and returns
// what to add to the mark's status: where it went, or why it stayed.
func (m *model) advance(move func()) string {
	before := m.status
	from := m.here()
	m.jump(move)
	switch {
	case m.status != before:
		return "; " + m.status
	case m.here() != from:
		return "; ctrl+o goes back"
	}
	return ""
}

// nextUnreviewedFile moves forward to the first unreviewed file after f, never back. With none
// after it, it moves on to the next unreviewed commit when this one is done, and otherwise stays,
// saying how many earlier files are left.
func (m *model) nextUnreviewedFile(f int) {
	v := m.views[m.current]
	for next := f + 1; next < len(v.files); next++ {
		if !isMessage(v.files[next]) && !m.fileReviewed(v, next) {
			m.focusFile(next)
			return
		}
	}
	left := 0
	for i := range f {
		if !isMessage(v.files[i]) && !m.fileReviewed(v, i) {
			left++
		}
	}
	switch {
	case left > 0:
		m.setStatus("%d earlier %s still unreviewed", left, map[bool]string{true: "file is", false: "files are"}[left == 1])
	case v.kind == viewCommit:
		m.nextUnreviewedCommit()
	default:
		m.setStatus("every file is reviewed")
	}
}

// nextUnreviewedCommit opens the next commit after this one not yet reviewed, at its first
// unreviewed file. It does not wrap back to earlier commits.
func (m *model) nextUnreviewedCommit() {
	for vi := m.current + 1; vi < len(m.views); vi++ {
		v := m.views[vi]
		if v.kind != viewCommit || m.viewReviewed(v) {
			continue
		}
		m.viewsNav.show(vi, -1)
		m.openView()
		for f := range v.files {
			if !isMessage(v.files[f]) && !m.fileReviewed(v, f) {
				m.focusFile(f)
				break
			}
		}
		// A commit opens at its top, its message in view, scrolled only as far as its file needs.
		m.main.offset = 0
		m.main.ensureVisible()
		return
	}
	m.setStatus("no unreviewed commit after this one; S to submit")
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
