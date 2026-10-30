package review

import (
	"context"
	"testing"

	"github.com/chancez/gr/internal/git"
)

// lines makes a one-file diff of n lines, weighing n+1 in the cache.
func lines(n int) []git.FileDiff {
	return []git.FileDiff{{NewPath: "f", Hunks: []git.Hunk{{Lines: make([]git.Line, n)}}}}
}

func TestDiffCacheEvictsByWeight(t *testing.T) {
	c := newDiffCache(25)
	c.put("a", lines(9)) // weight 10
	c.put("b", lines(9)) // 20
	if _, ok := c.get("a"); !ok {
		t.Fatal("a evicted early")
	}
	// a was used last, so b is the oldest when c pushes the total past 25.
	c.put("c", lines(9))
	if _, ok := c.get("b"); ok {
		t.Error("b, the least recently used, survived")
	}
	for _, k := range []string{"a", "c"} {
		if _, ok := c.get(k); !ok {
			t.Errorf("%s was evicted", k)
		}
	}
	if c.size != 20 {
		t.Errorf("size = %d, want 20", c.size)
	}
	// A diff bigger than the whole cache is kept on its own until the next one.
	c.put("huge", lines(100))
	if _, ok := c.get("huge"); !ok || c.order.Len() != 1 {
		t.Errorf("huge diff: cached %v, entries %d", ok, c.order.Len())
	}
}

// countingDiffs counts the reads that reach it.
type countingDiffs struct{ diffs, lists, logs int }

func (c *countingDiffs) Diff(context.Context, string, string, ...string) ([]git.FileDiff, error) {
	c.diffs++
	return lines(3), nil
}

func (c *countingDiffs) DiffFiles(context.Context, string, string) ([]git.FileDiff, error) {
	c.lists++
	return lines(0), nil
}

func (c *countingDiffs) CommitFiles(context.Context, string, string) (map[string][]git.FileDiff, error) {
	c.logs++
	return map[string][]git.FileDiff{"c": lines(0)}, nil
}

func TestCachedDiffsReadOnce(t *testing.T) {
	next := &countingDiffs{}
	d := NewCachedDiffs(next, 100)
	ctx := t.Context()
	for range 3 {
		if _, err := d.Diff(ctx, "a", "b", "f"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.DiffFiles(ctx, "a", "b"); err != nil {
			t.Fatal(err)
		}
		if _, err := d.CommitFiles(ctx, "a", "b"); err != nil {
			t.Fatal(err)
		}
	}
	// Another path is another diff.
	if _, err := d.Diff(ctx, "a", "b", "g"); err != nil {
		t.Fatal(err)
	}
	if *next != (countingDiffs{diffs: 2, lists: 1, logs: 1}) {
		t.Errorf("reads that reached git = %+v, want each read once", *next)
	}
}
