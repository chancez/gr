package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/review"
)

// editorProgram is the editor to run: $VISUAL, else $EDITOR, else vi.
func editorProgram() string {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if e := os.Getenv(v); e != "" {
			return e
		}
	}
	return "vi"
}

// oldRev is the revision the old side of v's files is read from, or "" when it has none to read.
func (m *model) oldRev(v view) string {
	switch v.kind {
	case viewCommit:
		return v.commit.SHA + "^"
	case viewDiff:
		return m.data.revision.BaseSHA
	}
	return ""
}

// openFileCmd opens file f of view v in $EDITOR at line, as it is on side there: a copy written from
// git to a temporary file, read-only, so the working tree is never touched and nothing is saved
// back.
func (m *model) openFileCmd(v view, f int, side anchor.Side, line int) tea.Cmd {
	cmd, done, err := m.editorCommand(v, f, side, line)
	if err != nil {
		m.setError(err)
		return nil
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		done()
		return editorMsg{err: err}
	})
}

// editorCommand writes file f of view v as it is on side to a temporary file, and is the command
// that opens it in $EDITOR at line, with done to remove the file after.
func (m *model) editorCommand(v view, f int, side anchor.Side, line int) (*exec.Cmd, func(), error) {
	file := v.files[f]
	rev, path := v.newRev, file.NewPath
	if side == anchor.SideOld || path == "" {
		rev, path = m.oldRev(v), file.OldPath
	}
	if rev == "" || path == "" || path == review.CommitMsgPath {
		return nil, nil, errors.New("no file to open here")
	}
	lines, err := m.svc.FileLines(m.ctx, rev, path)
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp("", "gr-")
	if err != nil {
		return nil, nil, err
	}
	done := func() { os.RemoveAll(dir) }
	// Named for the revision and the file, so the editor shows where it is from and highlights it.
	prefix := shortSHA(rev) + "-"
	if sha, ok := strings.CutSuffix(rev, "^"); ok {
		prefix = shortSHA(sha) + "-before-"
	}
	name := filepath.Join(dir, prefix+filepath.Base(path))
	if err := os.WriteFile(name, []byte(strings.Join(lines, "\n")+"\n"), 0o444); err != nil {
		done()
		return nil, nil, err
	}
	cmd := exec.Command("sh", "-c", editorProgram()+` +"$1" "$2"`, "sh", strconv.Itoa(max(line, 1)), name)
	return cmd, done, nil
}

// openingLine is the side and line a row opens at: a line's own, a hunk's first, or a fold's first.
func openingLine(s rowSubject) (anchor.Side, int) {
	r := s.row
	switch {
	case r.kind == rowLine:
		side := lineSide(r, s.side)
		return side, lineOn(r, side)
	case r.kind == rowHunk && r.hunk != nil:
		return anchor.SideNew, r.hunk.NewStart
	case r.gap != nil:
		return anchor.SideNew, r.gap.start
	}
	return anchor.SideNew, 1
}
