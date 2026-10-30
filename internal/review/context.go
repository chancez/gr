package review

import (
	"context"
	"fmt"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/store"
)

// ExcerptLines is how many lines before a thread's anchor an excerpt shows, as GitHub does.
const ExcerptLines = 3

// Excerpt is the part of a diff a thread is about: its anchored lines and a few lines before them.
type Excerpt struct {
	Path string
	Side anchor.Side
	// Hunk is the hunk header, empty when Exact is false.
	Hunk string
	// Lines are diff lines. When Exact is false they are all context, from the anchor's stored content.
	Lines []git.Line
	// Exact is true when the excerpt comes from the diff the thread is anchored to, and false when the
	// diff no longer has it, as for an outdated thread.
	Exact bool
}

// ExcerptFor cuts a thread's excerpt out of the files of the diff it is anchored to. It falls back
// to the anchor's stored content when files do not contain the anchor, and reports false for
// threads with no lines, such as change and file threads.
func ExcerptFor(files []git.FileDiff, loc store.Location) (Excerpt, bool) {
	if loc.StartLine == 0 || loc.Path == "" {
		return Excerpt{}, false
	}
	if f, ok := findFile(files, loc.Side, loc.Path); ok {
		for _, h := range f.Hunks {
			if e, ok := excerptFromHunk(loc, h); ok {
				return e, true
			}
		}
	}
	return storedExcerpt(loc), true
}

func lineOn(l git.Line, side anchor.Side) int {
	if side == anchor.SideOld {
		return l.OldLine
	}
	return l.NewLine
}

func excerptFromHunk(loc store.Location, h git.Hunk) (Excerpt, bool) {
	end := -1
	for i, l := range h.Lines {
		if lineOn(l, loc.Side) == loc.EndLine {
			end = i
		}
	}
	if end < 0 {
		return Excerpt{}, false
	}
	// Walk back to ExcerptLines lines before the start, counting lines on the anchor's side.
	start := end
	for start > 0 {
		n := lineOn(h.Lines[start-1], loc.Side)
		if n != 0 && n < loc.StartLine-ExcerptLines {
			break
		}
		start--
	}
	return Excerpt{
		Path:  loc.Path,
		Side:  loc.Side,
		Hunk:  fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldLines, h.NewStart, h.NewLines),
		Lines: append([]git.Line(nil), h.Lines[start:end+1]...),
		Exact: true,
	}, true
}

func storedExcerpt(loc store.Location) Excerpt {
	e := Excerpt{Path: loc.Path, Side: loc.Side}
	before := loc.Before[max(len(loc.Before)-ExcerptLines, 0):]
	first := loc.StartLine - len(before)
	for i, text := range append(append([]string(nil), before...), loc.Lines...) {
		n := first + i
		l := git.Line{Kind: git.LineContext, Text: text, NewLine: n}
		if loc.Side == anchor.SideOld {
			l = git.Line{Kind: git.LineContext, Text: text, OldLine: n}
		}
		e.Lines = append(e.Lines, l)
	}
	return e
}

// Excerpts returns each thread's excerpt, reading the diffs threads are anchored to once each.
// Threads without lines are left out of the map.
func (s *Service) Excerpts(ctx context.Context, threads []ThreadView) (map[string]Excerpt, error) {
	out := map[string]Excerpt{}
	diffs := map[string][]git.FileDiff{}
	for _, t := range threads {
		loc := t.Location
		if loc.StartLine == 0 || loc.Path == "" {
			continue
		}
		key := string(loc.Scope) + ":" + loc.Commit + ":" + loc.RevisionID
		files, ok := diffs[key]
		if !ok && !t.Outdated {
			var err error
			if files, err = s.locationFiles(ctx, loc); err != nil {
				return nil, err
			}
			diffs[key] = files
		}
		if e, ok := ExcerptFor(files, loc); ok {
			out[t.ID] = e
		}
	}
	return out, nil
}

// locationFiles reads the diff a location is anchored to.
func (s *Service) locationFiles(ctx context.Context, loc store.Location) ([]git.FileDiff, error) {
	switch loc.Scope {
	case store.ScopeCommit:
		c, err := s.git.ReadCommit(ctx, loc.Commit)
		if err != nil {
			return nil, err
		}
		return s.CommitFiles(ctx, c)
	case store.ScopeDiff:
		rev, err := s.store.GetRevision(ctx, loc.RevisionID)
		if err != nil {
			return nil, err
		}
		return s.RevisionFiles(ctx, rev)
	}
	return nil, nil
}
