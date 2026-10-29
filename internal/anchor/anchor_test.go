package anchor

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
)

func TestCapture(t *testing.T) {
	content := gittest.Numbered(10)
	got, err := Capture("f", SideNew, content, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := Anchor{
		Path: "f", Side: SideNew, StartLine: 2, EndLine: 3,
		Lines:  []string{"line 2", "line 3"},
		Before: []string{"line 1"},
		After:  []string{"line 4", "line 5", "line 6"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Capture mismatch (-want +got):\n%s", diff)
	}

	for _, r := range [][2]int{{0, 1}, {3, 2}, {10, 11}} {
		if _, err := Capture("f", SideNew, content, r[0], r[1]); err == nil {
			t.Errorf("Capture(%d, %d) succeeded, want error", r[0], r[1])
		}
	}
}

func TestFind(t *testing.T) {
	content := []string{"a", "b", "}", "c", "d", "}", "e"}
	braceAfterD, err := Capture("f", SideNew, content, 6, 6)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		anchor  Anchor
		content []string
		want    int
		wantOK  bool
	}{
		{
			name:    "unchanged",
			anchor:  braceAfterD,
			content: content,
			want:    6,
			wantOK:  true,
		},
		{
			name:    "shifted by inserted lines",
			anchor:  braceAfterD,
			content: slices.Concat([]string{"new 1", "new 2"}, content),
			want:    8,
			wantOK:  true,
		},
		{
			name:    "context picks the right duplicate",
			anchor:  braceAfterD,
			content: []string{"}", "c", "d", "}", "x"},
			want:    4,
			wantOK:  true,
		},
		{
			name:    "duplicates with no context are ambiguous",
			anchor:  braceAfterD,
			content: []string{"}", "x", "}"},
			wantOK:  false,
		},
		{
			name:    "a single match needs no context",
			anchor:  braceAfterD,
			content: []string{"x", "}", "y"},
			want:    2,
			wantOK:  true,
		},
		{
			name:    "lines changed",
			anchor:  braceAfterD,
			content: []string{"a", "b"},
			wantOK:  false,
		},
		{
			name:    "equal context ties go to the nearest",
			anchor:  Anchor{Lines: []string{"x"}, Before: []string{"p"}, StartLine: 5},
			content: []string{"p", "x", "q", "q", "p", "x"},
			want:    6,
			wantOK:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.anchor.Find(tt.content)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Find = %d, %v; want %d, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestMoved(t *testing.T) {
	content := []string{"a", "b", "c"}
	a, err := Capture("f", SideNew, content, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	shifted := []string{"z", "a", "b", "c"}
	got := a.Moved(shifted, 3)
	want := Anchor{Path: "f", Side: SideNew, StartLine: 3, EndLine: 3, Lines: []string{"b"}, Before: []string{"z", "a"}, After: []string{"c"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Moved mismatch (-want +got):\n%s", diff)
	}
}

func TestInDiff(t *testing.T) {
	f := git.FileDiff{Hunks: []git.Hunk{
		{OldStart: 10, OldLines: 3, NewStart: 12, NewLines: 5},
		{OldStart: 30, OldLines: 0, NewStart: 33, NewLines: 2},
	}}
	tests := []struct {
		side       Side
		start, end int
		want       bool
	}{
		{SideNew, 12, 12, true},
		{SideNew, 16, 16, true},
		{SideNew, 17, 20, false},
		{SideNew, 1, 40, true},
		{SideNew, 34, 34, true},
		{SideOld, 12, 12, true},
		{SideOld, 13, 13, false},
		// A hunk with no lines on the old side covers nothing there.
		{SideOld, 30, 30, false},
	}
	for _, tt := range tests {
		if got := InDiff(f, tt.side, tt.start, tt.end); got != tt.want {
			t.Errorf("InDiff(%s, %d, %d) = %v, want %v", tt.side, tt.start, tt.end, got, tt.want)
		}
	}
}
