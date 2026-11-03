package git_test

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/chancez/gr/internal/git"
	"github.com/chancez/gr/internal/gittest"
)

// TestFileListsMatchDiffs checks that the raw file lists name the same files and statuses as the
// full diffs, without hunks, with the same counts of added and deleted lines, and the same blobs and modes wherever the patch has them. A pure rename
// has no index line in a patch, so only the raw list carries its blobs.
func TestFileListsMatchDiffs(t *testing.T) {
	r := gittest.New(t)
	r.Write("keep.txt", "keep\n", "old name.txt", gittest.Lines(gittest.Numbered(20)...), "gone.txt", "bye\n")
	base := r.Commit("base")
	r.Write("keep.txt", "kept\n", "new.txt", "hi\n", "image.bin", "\x00\x01\x02")
	r.Git("mv", "old name.txt", "new name.txt")
	r.Git("rm", "-q", "gone.txt")
	one := r.Commit("one")
	r.Write("keep.txt", "kept again\n")
	two := r.Commit("two")
	repo := open(t, r.Dir)
	ctx := t.Context()

	noHunks := cmpopts.IgnoreFields(git.FileDiff{}, "Hunks")
	// fill copies blobs and modes from a list into a patch's pure renames, which lack them.
	fill := func(full, list []git.FileDiff) []git.FileDiff {
		out := append([]git.FileDiff(nil), full...)
		for i := range out {
			if out[i].Status == git.StatusRenamed && out[i].OldBlob == "" && i < len(list) {
				out[i].OldBlob, out[i].NewBlob = list[i].OldBlob, list[i].NewBlob
				out[i].OldMode, out[i].NewMode = list[i].OldMode, list[i].NewMode
			}
		}
		return out
	}
	lists, err := repo.CommitFiles(ctx, base, two)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{one, two} {
		commit, err := repo.ReadCommit(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		full, err := repo.CommitDiff(ctx, commit)
		if err != nil {
			t.Fatal(err)
		}
		if diff := cmp.Diff(fill(full, lists[c]), lists[c], noHunks); diff != "" {
			t.Errorf("commit %s file list mismatch (-diff +list):\n%s", c[:7], diff)
		}
	}
	// The rename's blobs, which only the raw list has, are the file's real blob on each side.
	for _, f := range lists[one] {
		if f.Status == git.StatusRenamed {
			if want := r.Git("rev-parse", base+":old name.txt"); f.OldBlob != want || f.NewBlob != want {
				t.Errorf("rename blobs = %s, %s; want %s", f.OldBlob, f.NewBlob, want)
			}
		}
	}
	if len(lists) != 2 {
		t.Errorf("file lists for %d commits, want 2", len(lists))
	}

	full, err := repo.Diff(ctx, base, two)
	if err != nil {
		t.Fatal(err)
	}
	list, err := repo.DiffFiles(ctx, base, two)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(fill(full, list), list, noHunks); diff != "" {
		t.Errorf("range file list mismatch (-diff +list):\n%s", diff)
	}
}
