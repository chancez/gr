package review_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/anchor"
	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

func TestExcerptFor(t *testing.T) {
	hunk := git.Hunk{OldStart: 1, OldLines: 7, NewStart: 1, NewLines: 7, Lines: []git.Line{
		{Kind: git.LineContext, Text: "1", OldLine: 1, NewLine: 1},
		{Kind: git.LineContext, Text: "2", OldLine: 2, NewLine: 2},
		{Kind: git.LineContext, Text: "3", OldLine: 3, NewLine: 3},
		{Kind: git.LineContext, Text: "4", OldLine: 4, NewLine: 4},
		{Kind: git.LineDeleted, Text: "five", OldLine: 5},
		{Kind: git.LineAdded, Text: "5", NewLine: 5},
		{Kind: git.LineContext, Text: "6", OldLine: 6, NewLine: 6},
		{Kind: git.LineContext, Text: "7", OldLine: 7, NewLine: 7},
	}}
	files := []git.FileDiff{{OldPath: "f", NewPath: "f", Hunks: []git.Hunk{hunk}}}
	loc := store.Location{Anchor: anchor.Anchor{Path: "f", Side: anchor.SideNew, StartLine: 5, EndLine: 6}}

	got, ok := review.ExcerptFor(files, loc)
	if !ok {
		t.Fatal("no excerpt")
	}
	want := review.Excerpt{
		Path: "f", Side: anchor.SideNew, Hunk: "@@ -1,7 +1,7 @@", Exact: true,
		// Three lines before line 5, the deletion between, then the anchored lines.
		Lines: hunk.Lines[1:7],
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ExcerptFor mismatch (-want +got):\n%s", diff)
	}

	// An anchor the diff no longer has falls back to its stored content.
	stale := store.Location{Anchor: anchor.Anchor{
		Path: "f", Side: anchor.SideNew, StartLine: 10, EndLine: 10,
		Lines: []string{"ten"}, Before: []string{"seven", "eight", "nine"},
	}}
	got, ok = review.ExcerptFor(files, stale)
	if !ok {
		t.Fatal("no excerpt for a stale anchor")
	}
	want = review.Excerpt{Path: "f", Side: anchor.SideNew, Lines: []git.Line{
		{Kind: git.LineContext, Text: "seven", NewLine: 7},
		{Kind: git.LineContext, Text: "eight", NewLine: 8},
		{Kind: git.LineContext, Text: "nine", NewLine: 9},
		{Kind: git.LineContext, Text: "ten", NewLine: 10},
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("stale ExcerptFor mismatch (-want +got):\n%s", diff)
	}

	if _, ok := review.ExcerptFor(files, store.Location{Scope: store.ScopeChange}); ok {
		t.Error("a change thread has an excerpt")
	}
}
