package tui

import (
	"strconv"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
)

// gap is a run of unchanged lines a diff leaves out: before the first hunk, between hunks, or after
// the last. It shows as a closed fold until opened.
type gap struct {
	index int
	// start and end are new-side line numbers. end is 0 for the gap after the last hunk, which runs to
	// the end of the file.
	start, end int
	// offset turns a new-side line number into the old side's.
	offset int
}

// hunkBounds returns the first and last line a hunk covers on one side. A side with no lines sits
// just after its start line, as git numbers it.
func hunkBounds(start, lines int) (first, last int) {
	if lines == 0 {
		return start + 1, start
	}
	return start, start + lines - 1
}

// gaps returns the unchanged runs around a file's hunks. Files without both sides have none: an
// added or deleted file is shown whole.
func gaps(f git.FileDiff) []gap {
	if f.Binary || f.OldPath == "" || f.NewPath == "" || f.NewPath == review.CommitMsgPath || len(f.Hunks) == 0 {
		return nil
	}
	var out []gap
	prevNew := 0
	for i, h := range f.Hunks {
		firstNew, lastNew := hunkBounds(h.NewStart, h.NewLines)
		firstOld, _ := hunkBounds(h.OldStart, h.OldLines)
		if firstNew > prevNew+1 {
			out = append(out, gap{index: i, start: prevNew + 1, end: firstNew - 1, offset: firstOld - firstNew})
		}
		prevNew = lastNew
	}
	last := f.Hunks[len(f.Hunks)-1]
	_, lastNew := hunkBounds(last.NewStart, last.NewLines)
	_, lastOld := hunkBounds(last.OldStart, last.OldLines)
	return append(out, gap{index: len(f.Hunks), start: lastNew + 1, offset: lastOld - lastNew})
}

// gapLines makes context lines for an open gap from the new side's content. It returns nil if the
// gap is empty, as the trailing gap is when the last hunk reaches the end of the file.
func gapLines(g gap, content []string) []git.Line {
	end := g.end
	if end == 0 || end > len(content) {
		end = len(content)
	}
	var lines []git.Line
	for n := g.start; n <= end; n++ {
		lines = append(lines, git.Line{Kind: git.LineContext, Text: content[n-1], OldLine: n + g.offset, NewLine: n})
	}
	return lines
}

// gapKey identifies a fold of unchanged lines across rebuilds.
func gapKey(v view, path string, g int) string {
	return v.key() + "\x00" + path + "\x00" + strconv.Itoa(g)
}

// gapLabel describes a closed fold.
func gapLabel(g gap) string {
	if g.end == 0 {
		return "... rest of file"
	}
	n := g.end - g.start + 1
	if n == 1 {
		return "... 1 unchanged line"
	}
	return "... " + strconv.Itoa(n) + " unchanged lines"
}
