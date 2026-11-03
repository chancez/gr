package tui

import (
	"fmt"
	"strings"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
	"github.com/chancez/gr/internal/tui/look"
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
	// newRev is the revision the new side of files is read from, for folded context.
	newRev string
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

// buildViews orders the sidebar: the overview, what changed since the last review, the commits
// oldest first, then the whole diff.
func buildViews(d *changeData) []view {
	head := d.revision.HeadSHA
	views := []view{{kind: viewOverview}}
	if d.hasInterdiff {
		views = append(views, view{kind: viewInterdiff, files: d.interdiff, newRev: d.interdiffTo.HeadSHA})
	}
	for _, c := range d.commits {
		views = append(views, view{kind: viewCommit, commit: c, files: d.commitFiles[c.SHA], newRev: c.SHA})
	}
	return append(views, view{kind: viewDiff, files: d.diffFiles, newRev: head})
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
	// rowGap is a closed fold of unchanged lines.
	rowGap
	// rowReview is a review in the overview: its header bar, then its summary when it has one.
	rowReview
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
	// reviewed and collapsed describe a rowFile.
	reviewed  bool
	collapsed bool
	// gap is set on a rowGap, and on the lines of an open gap.
	gap *gap
	// excerpt is the diff shown above a thread in the overview.
	excerpt *review.Excerpt
	// review is set on a rowReview. Its summary thread, if any, is thread.
	review *store.Review
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
	case rowGap:
		return fmt.Sprintf("gap:%s:%d", path, r.gap.index)
	case rowReview:
		return "review:" + r.review.ID
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

// rowOptions control how a file view is laid out.
type rowOptions struct {
	split bool
	// collapsed reports whether a file shows only its header.
	collapsed func(file int) bool
	// reviewed reports whether a file is marked reviewed.
	reviewed func(file int) bool
	// gapOpen reports whether a fold of unchanged lines is open.
	gapOpen func(file, gap int) bool
	// content returns a file's new-side lines, for open folds.
	content func(file int) []string
}

// buildRows lays out a file view, placing each thread after the line it ends on.
func buildRows(d *changeData, v view, opts rowOptions) []row {
	split := opts.split
	var rows []row
	byLine := map[threadKey][]*review.ThreadView{}
	byFile := map[string][]*review.ThreadView{}
	var whole, lineThreads []*review.ThreadView
	locs := map[*review.ThreadView]store.Location{}
	for i := range d.threads {
		t := &d.threads[i]
		loc, ok := d.locationIn(t, v)
		if !ok {
			continue
		}
		locs[t] = loc
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
		collapsed := opts.collapsed != nil && opts.collapsed(fi)
		rows = append(rows, row{
			kind: rowFile, file: fi, collapsed: collapsed,
			reviewed: opts.reviewed != nil && opts.reviewed(fi),
		})
		if collapsed {
			// The message's threads still show while its file is folded, as the header shows it.
			if f.Path() == review.CommitMsgPath {
				threadRows(fi, byFile[f.Path()])
				for _, t := range lineThreads {
					if locs[t].Path == f.Path() {
						threadRows(fi, []*review.ThreadView{t})
					}
				}
			}
			continue
		}
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
		fileGaps := gaps(*f)
		gapRows := func(before int) {
			for gi := range fileGaps {
				g := &fileGaps[gi]
				if g.index != before {
					continue
				}
				if opts.gapOpen == nil || !opts.gapOpen(fi, g.index) || opts.content == nil {
					rows = append(rows, row{kind: rowGap, file: fi, gap: g})
					continue
				}
				for _, l := range gapLines(*g, opts.content(fi)) {
					if split {
						rows = append(rows, row{kind: rowLine, file: fi, left: &l, right: &l, gap: g})
					} else {
						rows = append(rows, row{kind: rowLine, file: fi, line: &l, gap: g})
					}
				}
			}
		}
		for hi := range f.Hunks {
			h := &f.Hunks[hi]
			gapRows(hi)
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
		gapRows(len(f.Hunks))
		// Threads whose lines are not in this diff, which should not happen for current threads.
		for _, t := range lineThreads {
			loc := locs[t]
			onFile := (loc.Side == anchor.SideOld && loc.Path == f.OldPath) || (loc.Side == anchor.SideNew && loc.Path == f.NewPath)
			if onFile && !placed[t] {
				rows = append(rows, row{kind: rowThread, file: fi, thread: t})
			}
		}
	}
	return rows
}

// threadInView reports whether a current thread belongs in a file view.
func threadInView(t *review.ThreadView, v view, d *changeData) bool {
	_, ok := d.locationIn(t, v)
	return ok
}

// locationIn is where a current thread shows in a file view. Threads on a commit show in that
// commit, and in the whole diff at the place their lines map to at the head, when it is in the diff.
func (d *changeData) locationIn(t *review.ThreadView, v view) (store.Location, bool) {
	if t.Outdated {
		return store.Location{}, false
	}
	switch v.kind {
	case viewCommit:
		return t.Location, t.Location.Scope == store.ScopeCommit && t.Location.Commit == v.commit.SHA
	case viewDiff:
		if t.Location.Scope == store.ScopeDiff {
			return t.Location, true
		}
		loc, ok := d.diffPlacements[t.ID]
		return loc, ok
	}
	return store.Location{}, false
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

// searchText is the text of a row that search matches against.
func (r row) searchText(files []git.FileDiff) string {
	switch r.kind {
	case rowFile:
		return look.FileLabel(files[r.file])
	case rowHunk:
		return r.hunk.Section
	case rowLine:
		if r.line != nil {
			return r.line.Text
		}
		var parts []string
		for _, l := range []*git.Line{r.left, r.right} {
			if l != nil {
				parts = append(parts, l.Text)
			}
		}
		return strings.Join(parts, "\n")
	case rowThread, rowReview:
		if r.thread == nil {
			return ""
		}
		var parts []string
		for _, c := range r.thread.Comments {
			parts = append(parts, c.Author.Name, c.Body)
		}
		return strings.Join(parts, "\n")
	}
	return ""
}
