package tui

import (
	"fmt"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

type viewKind int

const (
	viewOverview viewKind = iota
	viewDiff
	viewCommit
	viewInterdiff
)

// view is one thing the main pane can show.
type view struct {
	kind   viewKind
	commit git.Commit
	files  []git.FileDiff
}

func (v view) key() string {
	switch v.kind {
	case viewCommit:
		return "commit:" + v.commit.SHA
	default:
		return fmt.Sprint(v.kind)
	}
}

func (v view) readOnly() bool { return v.kind == viewInterdiff }

func buildViews(d *changeData) []view {
	views := []view{{kind: viewOverview}, {kind: viewDiff, files: d.diffFiles}}
	for _, c := range d.commits {
		views = append(views, view{kind: viewCommit, commit: c, files: d.commitFiles[c.SHA]})
	}
	if d.interdiff != nil {
		views = append(views, view{kind: viewInterdiff, files: d.interdiff})
	}
	return views
}

type rowKind int

const (
	// rowText is static content: headings, review summaries.
	rowText rowKind = iota
	rowCommit
	rowFile
	rowHunk
	rowLine
	rowThread
)

// row is one selectable or static block of the main pane.
type row struct {
	kind rowKind
	// text renders a rowText at a width.
	text func(width int) []string
	// file indexes the view's files, or is -1.
	file int
	hunk *git.Hunk
	// line is set in unified mode. left and right are set in split mode, either may be nil.
	line        *git.Line
	left, right *git.Line
	thread      *review.ThreadView
}

func (r row) selectable() bool { return r.kind != rowText }

// key identifies a row across reloads, so the cursor stays put when data refreshes.
func (r row) key(files []git.FileDiff) string {
	path := ""
	if r.file >= 0 && r.file < len(files) {
		path = files[r.file].Path()
	}
	switch r.kind {
	case rowThread:
		return "thread:" + r.thread.ID
	case rowCommit:
		return "commit"
	case rowFile:
		return "file:" + path
	case rowHunk:
		return fmt.Sprintf("hunk:%s:%d:%d", path, r.hunk.OldStart, r.hunk.NewStart)
	case rowLine:
		old, new := lineNumbers(r)
		return fmt.Sprintf("line:%s:%d:%d", path, old, new)
	}
	return ""
}

func lineNumbers(r row) (old, new int) {
	if r.line != nil {
		return r.line.OldLine, r.line.NewLine
	}
	if r.left != nil {
		old = r.left.OldLine
	}
	if r.right != nil {
		new = r.right.NewLine
	}
	return old, new
}

// threadKey places line threads: the file's path on the thread's side, the side, and the end line.
type threadKey struct {
	path string
	side anchor.Side
	line int
}

// buildRows lays out a file view, placing each thread after the line it ends on.
func buildRows(d *changeData, v view, split bool) []row {
	var rows []row
	byLine := map[threadKey][]*review.ThreadView{}
	byFile := map[string][]*review.ThreadView{}
	var whole, lineThreads []*review.ThreadView
	for i := range d.threads {
		t := &d.threads[i]
		if !threadInView(t, v) {
			continue
		}
		loc := t.Location
		switch {
		case loc.Path == "":
			whole = append(whole, t)
		case loc.StartLine == 0:
			byFile[loc.Path] = append(byFile[loc.Path], t)
		default:
			k := threadKey{loc.Path, loc.Side, loc.EndLine}
			byLine[k] = append(byLine[k], t)
			lineThreads = append(lineThreads, t)
		}
	}
	threadRows := func(file int, ts []*review.ThreadView) {
		for _, t := range ts {
			rows = append(rows, row{kind: rowThread, file: file, thread: t})
		}
	}

	if v.kind == viewCommit {
		rows = append(rows, row{kind: rowCommit, file: -1})
		threadRows(-1, whole)
	}
	for fi := range v.files {
		f := &v.files[fi]
		rows = append(rows, row{kind: rowFile, file: fi})
		placed := map[*review.ThreadView]bool{}
		threadRows(fi, byFile[f.NewPath])
		if f.OldPath != f.NewPath {
			threadRows(fi, byFile[f.OldPath])
		}
		after := func(side anchor.Side, path string, line int) {
			if line == 0 || path == "" {
				return
			}
			ts := byLine[threadKey{path, side, line}]
			for _, t := range ts {
				placed[t] = true
			}
			threadRows(fi, ts)
		}
		for hi := range f.Hunks {
			h := &f.Hunks[hi]
			rows = append(rows, row{kind: rowHunk, file: fi, hunk: h})
			if split {
				for _, p := range pairLines(h.Lines) {
					rows = append(rows, row{kind: rowLine, file: fi, hunk: h, left: p[0], right: p[1]})
					if p[0] != nil {
						after(anchor.SideOld, f.OldPath, p[0].OldLine)
					}
					if p[1] != nil {
						after(anchor.SideNew, f.NewPath, p[1].NewLine)
					}
				}
				continue
			}
			for li := range h.Lines {
				l := &h.Lines[li]
				rows = append(rows, row{kind: rowLine, file: fi, hunk: h, line: l})
				after(anchor.SideOld, f.OldPath, l.OldLine)
				after(anchor.SideNew, f.NewPath, l.NewLine)
			}
		}
		// Threads whose lines are not in this diff, which should not happen for current threads.
		for _, t := range lineThreads {
			loc := t.Location
			onFile := (loc.Side == anchor.SideOld && loc.Path == f.OldPath) || (loc.Side == anchor.SideNew && loc.Path == f.NewPath)
			if onFile && !placed[t] {
				rows = append(rows, row{kind: rowThread, file: fi, thread: t})
			}
		}
	}
	return rows
}

// threadInView reports whether a current thread belongs in a file view.
func threadInView(t *review.ThreadView, v view) bool {
	if t.Outdated {
		return false
	}
	switch v.kind {
	case viewCommit:
		return t.Location.Scope == store.ScopeCommit && t.Location.Commit == v.commit.SHA
	case viewDiff:
		return t.Location.Scope == store.ScopeDiff
	}
	return false
}

// pairLines lines up a hunk for side-by-side display: context lines face themselves, and each run of
// deletions faces the run of additions that follows it.
func pairLines(lines []git.Line) [][2]*git.Line {
	var pairs [][2]*git.Line
	for i := 0; i < len(lines); {
		if lines[i].Kind == git.LineContext {
			pairs = append(pairs, [2]*git.Line{&lines[i], &lines[i]})
			i++
			continue
		}
		var dels, adds []*git.Line
		for i < len(lines) && lines[i].Kind == git.LineDeleted {
			dels = append(dels, &lines[i])
			i++
		}
		for i < len(lines) && lines[i].Kind == git.LineAdded {
			adds = append(adds, &lines[i])
			i++
		}
		for j := range max(len(dels), len(adds)) {
			var p [2]*git.Line
			if j < len(dels) {
				p[0] = dels[j]
			}
			if j < len(adds) {
				p[1] = adds[j]
			}
			pairs = append(pairs, p)
		}
	}
	return pairs
}
