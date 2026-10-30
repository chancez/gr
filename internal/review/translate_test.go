package review_test

import (
	"testing"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
	"github.com/chancez/gr/internal/review"
)

func TestMapLine(t *testing.T) {
	r := gittest.New(t)
	lines := gittest.Numbered(12)
	r.Write("f", gittest.Lines(lines...))
	from := r.Commit("from")
	// Insert two lines after 2, replace 6 and 7 with one line, delete 10.
	after := append(append([]string{}, lines[:2]...), "new a", "new b")
	after = append(after, lines[2:5]...)
	after = append(after, "replaced 6 and 7")
	after = append(after, lines[7:9]...)
	after = append(after, lines[10:]...)
	r.Write("f", gittest.Lines(after...))
	to := r.Commit("to")
	g, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	files, err := g.Diff(t.Context(), from, to, "f")
	if err != nil {
		t.Fatal(err)
	}
	f := &files[0]
	for line, want := range map[int]int{
		1:  1,  // before every change
		3:  5,  // shifted by the insertion
		5:  7,  // context inside a hunk
		6:  8,  // replaced: the start of its replacement
		7:  8,  // replaced
		8:  9,  // after the replacement: shifted by -1 more
		10: 11, // deleted: the line after
		12: 12, // after the deletion
	} {
		if got := review.MapLine(f, line); got != want {
			t.Errorf("MapLine(%d) = %d, want %d", line, got, want)
		}
	}
	if got := review.MapLine(nil, 7); got != 7 {
		t.Errorf("MapLine on an unchanged file = %d", got)
	}
}
