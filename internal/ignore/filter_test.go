package ignore

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFilter(t *testing.T) {
	paths := []string{"README.md", "docs/a.md", "main.go", "main_test.go", "vendor/x.go", "vendor/x.md"}
	for _, tc := range []struct {
		filter string
		want   []string
	}{
		{"", nil},
		{"*.md", []string{"main.go", "main_test.go", "vendor/x.go"}},
		{"!*_test.go", []string{"main_test.go"}},
		{"*.go !*_test.go", []string{"README.md", "docs/a.md", "main_test.go", "vendor/x.md"}},
		{"*.md  !vendor/", []string{"main.go", "main_test.go", "vendor/x.go", "vendor/x.md"}},
		{"docs/ main.go", []string{"README.md", "main_test.go", "vendor/x.go", "vendor/x.md"}},
	} {
		f := ParseFilter(tc.filter)
		var got []string
		for _, p := range paths {
			if f.Hides(p) {
				got = append(got, p)
			}
		}
		if diff := cmp.Diff(tc.want, got); diff != "" {
			t.Errorf("ParseFilter(%q) hides (-want +got):\n%s", tc.filter, diff)
		}
	}
	if got, want := ParseFilter("  *.md   !a.md "), "*.md !a.md"; got.String() != want {
		t.Errorf("String() = %q, want %q", got.String(), want)
	}
	if (Filter{}).Hides("main.go") {
		t.Error("the zero Filter hides main.go")
	}
}
