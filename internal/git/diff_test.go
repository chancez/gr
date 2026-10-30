package git

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseDiff(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []FileDiff
	}{
		{
			name: "modified",
			in: `diff --git a/main.go b/main.go
index 1111111..2222222 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@ package main
 a
-b
+B
 c
`,
			want: []FileDiff{{
				OldPath: "main.go",
				NewPath: "main.go",
				Status:  StatusModified,
				OldMode: "100644",
				NewMode: "100644",
				OldBlob: "1111111",
				NewBlob: "2222222",
				Hunks: []Hunk{{
					OldStart: 1, OldLines: 3, NewStart: 1, NewLines: 3,
					Section: "package main",
					Lines: []Line{
						{Kind: LineContext, Text: "a", OldLine: 1, NewLine: 1},
						{Kind: LineDeleted, Text: "b", OldLine: 2},
						{Kind: LineAdded, Text: "B", NewLine: 2},
						{Kind: LineContext, Text: "c", OldLine: 3, NewLine: 3},
					},
				}},
			}},
		},
		{
			name: "added without trailing newline",
			in: `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/new.txt
@@ -0,0 +1 @@
+hello
\ No newline at end of file
`,
			want: []FileDiff{{
				NewPath: "new.txt",
				Status:  StatusAdded,
				NewMode: "100644",
				NewBlob: "3333333",
				Hunks: []Hunk{{
					OldStart: 0, OldLines: 0, NewStart: 1, NewLines: 1,
					Lines: []Line{{Kind: LineAdded, Text: "hello", NewLine: 1, NoNewline: true}},
				}},
			}},
		},
		{
			name: "deleted",
			in: `diff --git a/old.txt b/old.txt
deleted file mode 100644
index 3333333..0000000
--- a/old.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-x
-y
`,
			want: []FileDiff{{
				OldPath: "old.txt",
				Status:  StatusDeleted,
				OldMode: "100644",
				OldBlob: "3333333",
				Hunks: []Hunk{{
					OldStart: 1, OldLines: 2, NewStart: 0, NewLines: 0,
					Lines: []Line{
						{Kind: LineDeleted, Text: "x", OldLine: 1},
						{Kind: LineDeleted, Text: "y", OldLine: 2},
					},
				}},
			}},
		},
		{
			name: "pure rename",
			in: `diff --git a/a b/c d b/e f
similarity index 100%
rename from a b/c d
rename to e f
`,
			want: []FileDiff{{OldPath: "a b/c d", NewPath: "e f", Status: StatusRenamed}},
		},
		{
			name: "binary and quoted path",
			in: `diff --git "a/caf\303\251\t.bin" "b/caf\303\251\t.bin"
index 1111111..2222222 100644
Binary files "a/caf\303\251\t.bin" and "b/caf\303\251\t.bin" differ
`,
			want: []FileDiff{{
				OldPath: "caf\u00e9\t.bin",
				NewPath: "caf\u00e9\t.bin",
				Status:  StatusModified,
				OldMode: "100644",
				NewMode: "100644",
				OldBlob: "1111111",
				NewBlob: "2222222",
				Binary:  true,
			}},
		},
		{
			name: "mode change only, path with spaces",
			in: `diff --git a/x y b/x y
old mode 100644
new mode 100755
`,
			want: []FileDiff{{OldPath: "x y", NewPath: "x y", Status: StatusModified, OldMode: "100644", NewMode: "100755"}},
		},
		{
			name: "two files, line that looks like a header inside a hunk",
			in: `diff --git a/a b/a
--- a/a
+++ b/a
@@ -1 +1 @@
---- a/x
+++++ b/x
diff --git a/b b/b
--- a/b
+++ b/b
@@ -1,0 +2 @@
+z
`,
			want: []FileDiff{
				{
					OldPath: "a", NewPath: "a", Status: StatusModified,
					Hunks: []Hunk{{
						OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 1,
						Lines: []Line{
							{Kind: LineDeleted, Text: "--- a/x", OldLine: 1},
							{Kind: LineAdded, Text: "++++ b/x", NewLine: 1},
						},
					}},
				},
				{
					OldPath: "b", NewPath: "b", Status: StatusModified,
					Hunks: []Hunk{{
						OldStart: 1, OldLines: 0, NewStart: 2, NewLines: 1,
						Lines: []Line{{Kind: LineAdded, Text: "z", NewLine: 2}},
					}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDiff([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseDiff mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
