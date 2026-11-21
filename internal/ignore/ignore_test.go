package ignore

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/chancez/gr/internal/gittest"
)

func TestMatch(t *testing.T) {
	paths := []string{
		"main.go",
		"vendor/a.go",
		"vendor/b/c.go",
		"lib/vendor/d.go",
		"vendor",
		"keep/vendor/e.go",
		"gen/x.pb.go",
		"x.pb.go",
		"docs/a/b/c.md",
		"docs/c.md",
		"build/out",
		"src/build",
		"lit #x",
		"lit!",
		"a[b].txt",
		"ab.txt",
		"trail ",
		"crlf.txt",
	}
	tests := []struct {
		name  string
		lines string
		want  []string
	}{
		{
			name:  "directory at any depth",
			lines: "vendor/\n",
			want:  []string{"vendor/a.go", "vendor/b/c.go", "lib/vendor/d.go", "keep/vendor/e.go"},
		},
		{
			name:  "anchored directory",
			lines: "/vendor/\n",
			want:  []string{"vendor/a.go", "vendor/b/c.go"},
		},
		{
			name:  "name matches files and directories",
			lines: "build\n",
			want:  []string{"build/out", "src/build"},
		},
		{
			name:  "star stays within a directory",
			lines: "*.pb.go\ndocs/*.md\n",
			want:  []string{"gen/x.pb.go", "x.pb.go", "docs/c.md"},
		},
		{
			name:  "double star",
			lines: "docs/**/c.md\n**/b/*.go\n",
			want:  []string{"vendor/b/c.go", "docs/a/b/c.md", "docs/c.md"},
		},
		{
			name:  "trailing double star",
			lines: "vendor/**\n",
			want:  []string{"vendor/a.go", "vendor/b/c.go"},
		},
		{
			name:  "negation re-includes a file",
			lines: "vendor/*\n!vendor/a.go\n",
			want:  []string{"vendor/b/c.go"},
		},
		{
			name:  "negation cannot re-include a file in an ignored directory",
			lines: "vendor/\n!vendor/a.go\n!keep/vendor/e.go\n",
			want:  []string{"vendor/a.go", "vendor/b/c.go", "lib/vendor/d.go", "keep/vendor/e.go"},
		},
		{
			name:  "comments, blanks, and escapes",
			lines: "# vendor/\n\n\\#x\nlit\\ \\#x\nlit\\!\ntrail\\ \n",
			want:  []string{"lit #x", "lit!", "trail "},
		},
		{
			name:  "classes",
			lines: "a[\\[]b].txt\n[!m]ain.go\n[a-c]b.txt\n",
			want:  []string{"a[b].txt", "ab.txt"},
		},
		{
			name:  "crlf",
			lines: "crlf.txt\r\n",
			want:  []string{"crlf.txt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Parse([]byte(tt.lines))
			var got []string
			for _, p := range paths {
				if m.Match(p) {
					got = append(got, p)
				}
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Match (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.want, gitIgnored(t, tt.lines, paths)); diff != "" {
				t.Errorf("git check-ignore disagrees with want (-want +git):\n%s", diff)
			}
		})
	}
}

// gitIgnored is which of paths git ignores with lines as the repository's .gitignore, in order.
func gitIgnored(t *testing.T, lines string, paths []string) []string {
	t.Helper()
	r := gittest.New(t)
	if err := os.WriteFile(filepath.Join(r.Dir, ".gitignore"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "check-ignore", "--no-index", "--stdin", "-z")
	cmd.Dir = r.Dir
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	out, err := cmd.Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global")
	repo := filepath.Join(dir, "repo")
	local := filepath.Join(dir, "local")
	for path, lines := range map[string]string{
		global: "*.lock\n",
		repo:   "vendor/*\n",
		local:  "!vendor/mine.go\n!go.lock\n",
	} {
		if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Load(global, repo, filepath.Join(dir, "missing"), local)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range []string{"Cargo.lock", "go.lock", "vendor/x.go", "vendor/mine.go", "main.go"} {
		if m.Match(p) {
			got = append(got, p)
		}
	}
	if diff := cmp.Diff([]string{"Cargo.lock", "vendor/x.go"}, got); diff != "" {
		t.Errorf("Match (-want +got):\n%s", diff)
	}
}
