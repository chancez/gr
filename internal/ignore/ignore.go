// Package ignore matches paths against patterns in gitignore's syntax, for the files a review
// leaves collapsed and the files a filter hides.
package ignore

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

// Matcher is a list of patterns, the last match winning.
type Matcher struct {
	patterns []pattern
}

type pattern struct {
	re *regexp.Regexp
	// negate re-includes what the pattern matches, for a line starting with !.
	negate bool
	// dirOnly matches only directories, for a line ending with /.
	dirOnly bool
}

// Load reads the files at paths in order, so a later file's patterns override an earlier's. A
// missing file has no patterns.
func Load(paths ...string) (*Matcher, error) {
	m := &Matcher{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		m.patterns = append(m.patterns, Parse(data).patterns...)
	}
	return m, nil
}

// Parse reads patterns from the contents of an ignore file.
func Parse(data []byte) *Matcher {
	m := &Matcher{}
	for line := range bytes.Lines(data) {
		if p, ok := parseLine(strings.TrimRight(string(line), "\r\n")); ok {
			m.patterns = append(m.patterns, p)
		}
	}
	return m
}

func parseLine(line string) (pattern, bool) {
	line = trimTrailingSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return pattern{}, false
	}
	var p pattern
	if strings.HasPrefix(line, "!") {
		p.negate = true
		line = line[1:]
	}
	if s, ok := strings.CutSuffix(line, "/"); ok {
		p.dirOnly = true
		line = s
	}
	if line == "" {
		return pattern{}, false
	}
	// A slash other than a trailing one anchors the pattern to the root; otherwise it matches at
	// any depth.
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	var re strings.Builder
	re.WriteString("^")
	if !anchored {
		re.WriteString("(?:.*/)?")
	}
	re.WriteString(translate(line))
	re.WriteString("$")
	p.re = regexp.MustCompile(re.String())
	return p, true
}

// trimTrailingSpace drops trailing spaces unless escaped with a backslash.
func trimTrailingSpace(s string) string {
	for strings.HasSuffix(s, " ") && !strings.HasSuffix(s, `\ `) {
		s = s[:len(s)-1]
	}
	return s
}

// translate turns a glob into a regular expression: ** spans directories, * and ? do not, and
// [...] is a class.
func translate(glob string) string {
	var re strings.Builder
	segs := strings.Split(glob, "/")
	for i, seg := range segs {
		last := i == len(segs)-1
		if seg == "**" {
			switch {
			case last && i == 0:
				re.WriteString(".*")
			case last:
				// a/** is everything inside a.
				re.WriteString(".+")
			default:
				// **/ is any number of directories, none included.
				re.WriteString("(?:.*/)?")
			}
			continue
		}
		re.WriteString(translateSegment(seg))
		if !last {
			re.WriteString("/")
		}
	}
	return re.String()
}

func translateSegment(seg string) string {
	var re strings.Builder
	for i := 0; i < len(seg); i++ {
		switch c := seg[i]; c {
		case '*':
			for i+1 < len(seg) && seg[i+1] == '*' {
				i++
			}
			re.WriteString("[^/]*")
		case '?':
			re.WriteString("[^/]")
		case '\\':
			if i+1 < len(seg) {
				i++
			}
			re.WriteString(regexp.QuoteMeta(seg[i : i+1]))
		case '[':
			class, n, ok := translateClass(seg[i:])
			if !ok {
				re.WriteString(`\[`)
				continue
			}
			re.WriteString(class)
			i += n - 1
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return re.String()
}

// translateClass translates the bracket expression at the start of s, reporting how many bytes it
// took, or false if it is not closed.
func translateClass(s string) (string, int, bool) {
	var re strings.Builder
	re.WriteString("[")
	i := 1
	if i < len(s) && (s[i] == '!' || s[i] == '^') {
		re.WriteString("^/")
		i++
	}
	// A ] first is a member, not the end.
	if i < len(s) && s[i] == ']' {
		re.WriteString(`\]`)
		i++
	}
	for ; i < len(s); i++ {
		switch c := s[i]; c {
		case ']':
			re.WriteString("]")
			return re.String(), i + 1, true
		case '\\':
			if i+1 < len(s) {
				i++
				re.WriteString(regexp.QuoteMeta(s[i : i+1]))
			}
		case '-':
			re.WriteString("-")
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return "", 0, false
}

// Match reports whether the file at path, relative to the repository's root with slashes, is
// ignored. As in git, a file in an ignored directory cannot be re-included.
func (m *Matcher) Match(path string) bool {
	if m == nil || len(m.patterns) == 0 {
		return false
	}
	for i := range len(path) {
		if path[i] == '/' && m.match(path[:i], true) {
			return true
		}
	}
	return m.match(path, false)
}

func (m *Matcher) match(path string, dir bool) bool {
	for i := len(m.patterns) - 1; i >= 0; i-- {
		p := m.patterns[i]
		if p.dirOnly && !dir {
			continue
		}
		if p.re.MatchString(path) {
			return !p.negate
		}
	}
	return false
}
