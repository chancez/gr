package ignore

import "strings"

// Filter picks the files a review shows by their paths: a file shows when it matches a pattern to
// include, if there is one, and no pattern to exclude. The zero Filter shows every file.
type Filter struct {
	text    string
	include *Matcher
	exclude *Matcher
}

// ParseFilter reads a filter's patterns, separated by spaces, each in gitignore's syntax: "*.md"
// shows only Markdown files, and "!*_test.go" hides Go tests.
func ParseFilter(s string) Filter {
	fields := strings.Fields(s)
	var include, exclude []string
	for _, f := range fields {
		if p, ok := strings.CutPrefix(f, "!"); ok {
			exclude = append(exclude, p)
		} else {
			include = append(include, f)
		}
	}
	return Filter{
		text:    strings.Join(fields, " "),
		include: Parse([]byte(strings.Join(include, "\n"))),
		exclude: Parse([]byte(strings.Join(exclude, "\n"))),
	}
}

// Hides reports whether the filter hides the file at path.
func (f Filter) Hides(path string) bool {
	if f.exclude.Match(path) {
		return true
	}
	return f.include != nil && len(f.include.patterns) > 0 && !f.include.Match(path)
}

// String is the filter's patterns, as ParseFilter reads them.
func (f Filter) String() string { return f.text }
