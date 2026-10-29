// Package anchor captures the content a thread points at and finds it again in other versions of a
// file.
package anchor

import (
	"fmt"
	"slices"
	"strings"

	"github.com/chancez/gr/internal/git"
)

// Side is which side of a diff an anchor is on.
type Side string

const (
	SideOld Side = "old"
	SideNew Side = "new"
)

// ContextLines is how many lines before and after the anchored range are captured.
const ContextLines = 3

// Anchor is a line range in one file on one side of a diff, with the content needed to find it
// again.
type Anchor struct {
	Path      string
	Side      Side
	StartLine int
	EndLine   int
	Lines     []string
	Before    []string
	After     []string
}

// SplitLines splits file content into lines, without line terminators.
func SplitLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
}

// Capture builds an anchor for lines start through end, 1-based and inclusive, of content.
func Capture(path string, side Side, content []string, start, end int) (Anchor, error) {
	if start < 1 || end < start || end > len(content) {
		return Anchor{}, fmt.Errorf("line range %d-%d is outside %s, which has %d lines", start, end, path, len(content))
	}
	return Anchor{
		Path:      path,
		Side:      side,
		StartLine: start,
		EndLine:   end,
		Lines:     clone(content[start-1 : end]),
		Before:    clone(content[max(0, start-1-ContextLines) : start-1]),
		After:     clone(content[end:min(len(content), end+ContextLines)]),
	}, nil
}

// clone copies s, returning nil for an empty range so anchors compare equal whatever they were cut
// from.
func clone(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return slices.Clone(s)
}

// Find locates the anchored lines in content and returns the 1-based start line of the best match.
//
// Candidates are exact matches of the anchored lines, scored by how many context lines also match,
// counting outward from the range. The highest score wins, and ties go to the candidate nearest the
// original line. When several candidates exist and none has matching context, the match is
// ambiguous and Find reports false.
func (a Anchor) Find(content []string) (int, bool) {
	n := len(a.Lines)
	if n == 0 || n > len(content) {
		return 0, false
	}
	best, bestScore, candidates := 0, -1, 0
	for i := 0; i+n <= len(content); i++ {
		if !slices.Equal(content[i:i+n], a.Lines) {
			continue
		}
		candidates++
		start := i + 1
		score := a.contextScore(content, i)
		if score > bestScore || (score == bestScore && distance(start, a.StartLine) < distance(best, a.StartLine)) {
			best, bestScore = start, score
		}
	}
	if candidates == 0 || (candidates > 1 && bestScore == 0) {
		return 0, false
	}
	return best, true
}

func (a Anchor) contextScore(content []string, i int) int {
	score := 0
	for k := range a.Before {
		j := i - 1 - k
		if j < 0 || content[j] != a.Before[len(a.Before)-1-k] {
			break
		}
		score++
	}
	end := i + len(a.Lines)
	for k, line := range a.After {
		j := end + k
		if j >= len(content) || content[j] != line {
			break
		}
		score++
	}
	return score
}

func distance(a, b int) int {
	if a > b {
		return a - b
	}
	return b - a
}

// Moved returns a copy of the anchor at a new start line in content, with its context recaptured.
func (a Anchor) Moved(content []string, start int) Anchor {
	moved, err := Capture(a.Path, a.Side, content, start, start+len(a.Lines)-1)
	if err != nil {
		// Find only returns ranges that fit in content.
		panic(err)
	}
	return moved
}

// InDiff reports whether lines start through end on side overlap a hunk of f.
func InDiff(f git.FileDiff, side Side, start, end int) bool {
	for _, h := range f.Hunks {
		hStart, hLines := h.NewStart, h.NewLines
		if side == SideOld {
			hStart, hLines = h.OldStart, h.OldLines
		}
		if hLines == 0 {
			continue
		}
		if start <= hStart+hLines-1 && end >= hStart {
			return true
		}
	}
	return false
}
