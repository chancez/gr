package review

import (
	"context"

	"github.com/chancez/gr/internal/git"
)

// MapLine maps a line of a file to the same place after the change in f, a diff of that one file.
// Unchanged lines map exactly. A line the diff changed maps to the start of what replaced it, or,
// if it was only deleted, to the line after. A nil f means the file did not change.
func MapLine(f *git.FileDiff, line int) int {
	if f == nil {
		return line
	}
	shift := 0
	for _, h := range f.Hunks {
		oldFirst, oldLast := h.OldStart, h.OldStart+h.OldLines-1
		if h.OldLines == 0 {
			// A pure insertion after line OldStart.
			oldFirst, oldLast = h.OldStart+1, h.OldStart
		}
		if line < oldFirst {
			break
		}
		if line > oldLast {
			shift = (h.NewStart + h.NewLines) - (h.OldStart + h.OldLines)
			if h.OldLines == 0 {
				shift = h.NewLines
			}
			continue
		}
		// Inside the hunk: a context line keeps its place; a changed line goes to the first new-side
		// line after it.
		passed := false
		for _, l := range h.Lines {
			if !passed {
				if l.OldLine == line && l.Kind == git.LineContext {
					return l.NewLine
				}
				passed = l.OldLine == line
				continue
			}
			if l.NewLine != 0 {
				return l.NewLine
			}
		}
		return h.NewStart + h.NewLines
	}
	return line + shift
}

// mapLineEnd is MapLine for the last line of a range: a line the diff changed maps to the end of
// what replaced it, so a range keeps all of its replacement.
func mapLineEnd(f *git.FileDiff, line int) int {
	start := MapLine(f, line)
	if f == nil {
		return start
	}
	for _, h := range f.Hunks {
		for i, l := range h.Lines {
			if l.OldLine != line || l.Kind != git.LineDeleted {
				continue
			}
			end := 0
			for _, next := range h.Lines[i+1:] {
				if next.Kind == git.LineContext {
					break
				}
				if next.Kind == git.LineAdded {
					end = next.NewLine
				}
			}
			return max(end, start)
		}
	}
	return start
}

// translateRange maps lines start to end of path at rev from to what is in their place at rev to,
// with all of what replaced them.
func (s *Service) translateRange(ctx context.Context, from, to, path string, start, end int) (int, int, error) {
	files, err := s.diffs.Diff(ctx, from, to, path)
	if err != nil {
		return 0, 0, err
	}
	var f *git.FileDiff
	for i := range files {
		if files[i].OldPath == path {
			f = &files[i]
		}
	}
	first := MapLine(f, start)
	return first, max(mapLineEnd(f, end), first), nil
}

// TranslateLines maps lines start to end of path at rev from to the same place at rev to.
func (s *Service) TranslateLines(ctx context.Context, from, to, path string, start, end int) (int, int, error) {
	if from == to {
		return start, end, nil
	}
	files, err := s.diffs.Diff(ctx, from, to, path)
	if err != nil {
		return 0, 0, err
	}
	var f *git.FileDiff
	for i := range files {
		if files[i].OldPath == path {
			f = &files[i]
		}
	}
	return MapLine(f, start), max(MapLine(f, end), MapLine(f, start)), nil
}
