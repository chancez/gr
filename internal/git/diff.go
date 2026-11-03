package git

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// FileStatus is how a file changed.
type FileStatus string

const (
	StatusModified FileStatus = "modified"
	StatusAdded    FileStatus = "added"
	StatusDeleted  FileStatus = "deleted"
	StatusRenamed  FileStatus = "renamed"
	StatusCopied   FileStatus = "copied"
)

// FileDiff is the change to one file.
type FileDiff struct {
	// OldPath is empty for an added file.
	OldPath string
	// NewPath is empty for a deleted file.
	NewPath string
	Status  FileStatus
	OldMode string
	NewMode string
	// OldBlob and NewBlob are the blob ids from the index line, empty for a side that does not exist
	// or when git printed no index line, as for a pure rename.
	OldBlob string
	NewBlob string
	Binary  bool
	// Added and Deleted count the file's added and deleted lines, from --numstat for a file list or
	// from its hunks.
	Added   int
	Deleted int
	Hunks   []Hunk
}

// Path is the file's path on the new side, or the old side for a deleted file.
func (f FileDiff) Path() string {
	if f.NewPath != "" {
		return f.NewPath
	}
	return f.OldPath
}

// Hunk is one contiguous region of change.
type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	// Section is the text after the second @@, usually the enclosing function.
	Section string
	Lines   []Line
}

// LineKind is whether a line is unchanged, added, or deleted.
type LineKind string

const (
	LineContext LineKind = "context"
	LineAdded   LineKind = "added"
	LineDeleted LineKind = "deleted"
)

// Line is one line of a hunk. OldLine is 0 for added lines and NewLine is 0 for deleted ones.
type Line struct {
	Kind    LineKind
	Text    string
	OldLine int
	NewLine int
	// NoNewline is set when the line has no trailing newline in its file.
	NoNewline bool
}

// ParseDiff parses the output of git diff with a/ and b/ prefixes.
func ParseDiff(out []byte) ([]FileDiff, error) {
	p := &diffParser{lines: splitLines(out)}
	return p.parse()
}

func splitLines(out []byte) []string {
	if len(out) == 0 {
		return nil
	}
	out = bytes.TrimSuffix(out, []byte("\n"))
	return strings.Split(string(out), "\n")
}

type diffParser struct {
	lines []string
	pos   int
}

func (p *diffParser) parse() ([]FileDiff, error) {
	var files []FileDiff
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if !strings.HasPrefix(line, "diff --git ") {
			return nil, fmt.Errorf("line %d: expected diff header, got %q", p.pos+1, line)
		}
		f, err := p.parseFile()
		if err != nil {
			return nil, err
		}
		for _, h := range f.Hunks {
			for _, l := range h.Lines {
				switch l.Kind {
				case LineAdded:
					f.Added++
				case LineDeleted:
					f.Deleted++
				}
			}
		}
		files = append(files, f)
	}
	return files, nil
}

func (p *diffParser) parseFile() (FileDiff, error) {
	header := p.lines[p.pos]
	p.pos++
	f := FileDiff{Status: StatusModified}
	var minus, plus string
	var haveMinus, havePlus bool
	for p.pos < len(p.lines) {
		line := p.lines[p.pos]
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "@@ ") {
			break
		}
		p.pos++
		switch {
		case strings.HasPrefix(line, "--- "):
			minus, haveMinus = line[4:], true
		case strings.HasPrefix(line, "+++ "):
			plus, havePlus = line[4:], true
		case strings.HasPrefix(line, "new file mode "):
			f.Status = StatusAdded
			f.NewMode = line[len("new file mode "):]
		case strings.HasPrefix(line, "deleted file mode "):
			f.Status = StatusDeleted
			f.OldMode = line[len("deleted file mode "):]
		case strings.HasPrefix(line, "old mode "):
			f.OldMode = line[len("old mode "):]
		case strings.HasPrefix(line, "new mode "):
			f.NewMode = line[len("new mode "):]
		case strings.HasPrefix(line, "rename from "):
			f.Status = StatusRenamed
			f.OldPath = unquote(line[len("rename from "):])
		case strings.HasPrefix(line, "rename to "):
			f.NewPath = unquote(line[len("rename to "):])
		case strings.HasPrefix(line, "copy from "):
			f.Status = StatusCopied
			f.OldPath = unquote(line[len("copy from "):])
		case strings.HasPrefix(line, "copy to "):
			f.NewPath = unquote(line[len("copy to "):])
		case strings.HasPrefix(line, "Binary files ") || line == "GIT binary patch":
			f.Binary = true
		case strings.HasPrefix(line, "index "):
			// "index abc..def 100644": the mode is shared by both sides.
			blobs, mode, hasMode := strings.Cut(line[len("index "):], " ")
			if hasMode {
				f.OldMode, f.NewMode = mode, mode
			}
			if old, new, ok := strings.Cut(blobs, ".."); ok {
				f.OldBlob, f.NewBlob = nonZero(old), nonZero(new)
			}
		}
	}

	if haveMinus {
		f.OldPath = stripPrefix(minus, "a/")
	}
	if havePlus {
		f.NewPath = stripPrefix(plus, "b/")
	}
	if !haveMinus && !havePlus && f.OldPath == "" && f.NewPath == "" {
		// No ---/+++ lines and no rename: binary or mode-only. Both sides name the same path.
		path, err := headerPath(header)
		if err != nil {
			return FileDiff{}, err
		}
		f.OldPath, f.NewPath = path, path
	}
	switch f.Status {
	case StatusAdded:
		f.OldPath = ""
	case StatusDeleted:
		f.NewPath = ""
	}

	for p.pos < len(p.lines) && strings.HasPrefix(p.lines[p.pos], "@@ ") {
		h, err := p.parseHunk()
		if err != nil {
			return FileDiff{}, err
		}
		f.Hunks = append(f.Hunks, h)
	}
	return f, nil
}

// nonZero returns id, or "" for the all-zero id git prints for a missing side.
func nonZero(id string) string {
	if strings.Trim(id, "0") == "" {
		return ""
	}
	return id
}

// stripPrefix removes git's a/ or b/ prefix from a ---/+++ path. /dev/null becomes "".
func stripPrefix(s, prefix string) string {
	s = unquote(strings.TrimSuffix(s, "\t"))
	if s == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(s, prefix)
}

// headerPath extracts the path from "diff --git a/P b/P", where both sides are the same path.
func headerPath(header string) (string, error) {
	rest := strings.TrimPrefix(header, "diff --git ")
	if strings.HasPrefix(rest, `"`) {
		// Quoted: `"a/P" "b/P"`. The two halves have equal length.
		half := (len(rest) - 1) / 2
		return strings.TrimPrefix(unquote(rest[:half]), "a/"), nil
	}
	// Unquoted: "a/P b/P" has length 2*len(P)+5.
	n := len(rest) - 5
	if n <= 0 || n%2 != 0 {
		return "", fmt.Errorf("cannot parse diff header %q", header)
	}
	return rest[2 : 2+n/2], nil
}

// unquote undoes git's C-style path quoting, which Go's string literal syntax covers.
func unquote(s string) string {
	if len(s) < 2 || s[0] != '"' {
		return s
	}
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

func (p *diffParser) parseHunk() (Hunk, error) {
	header := p.lines[p.pos]
	p.pos++
	var h Hunk
	if err := parseHunkHeader(header, &h); err != nil {
		return Hunk{}, fmt.Errorf("line %d: %w", p.pos, err)
	}
	oldLine, newLine := h.OldStart, h.NewStart
	oldLeft, newLeft := h.OldLines, h.NewLines
	for p.pos < len(p.lines) && (oldLeft > 0 || newLeft > 0 || strings.HasPrefix(p.lines[p.pos], `\`)) {
		line := p.lines[p.pos]
		p.pos++
		if line == "" {
			// Some tools strip the trailing space of an empty context line.
			line = " "
		}
		switch line[0] {
		case ' ':
			h.Lines = append(h.Lines, Line{Kind: LineContext, Text: line[1:], OldLine: oldLine, NewLine: newLine})
			oldLine++
			newLine++
			oldLeft--
			newLeft--
		case '-':
			h.Lines = append(h.Lines, Line{Kind: LineDeleted, Text: line[1:], OldLine: oldLine})
			oldLine++
			oldLeft--
		case '+':
			h.Lines = append(h.Lines, Line{Kind: LineAdded, Text: line[1:], NewLine: newLine})
			newLine++
			newLeft--
		case '\\':
			if len(h.Lines) > 0 {
				h.Lines[len(h.Lines)-1].NoNewline = true
			}
		default:
			return Hunk{}, fmt.Errorf("line %d: unexpected hunk line %q", p.pos, line)
		}
	}
	return h, nil
}

// parseHunkHeader parses "@@ -a,b +c,d @@ section".
func parseHunkHeader(header string, h *Hunk) error {
	rest, ok := strings.CutPrefix(header, "@@ ")
	if !ok {
		return fmt.Errorf("malformed hunk header %q", header)
	}
	ranges, section, ok := strings.Cut(rest, " @@")
	if !ok {
		return fmt.Errorf("malformed hunk header %q", header)
	}
	h.Section = strings.TrimPrefix(section, " ")
	oldRange, newRange, ok := strings.Cut(ranges, " ")
	if !ok {
		return fmt.Errorf("malformed hunk header %q", header)
	}
	var err error
	if h.OldStart, h.OldLines, err = parseRange(oldRange, "-"); err != nil {
		return fmt.Errorf("hunk header %q: %w", header, err)
	}
	if h.NewStart, h.NewLines, err = parseRange(newRange, "+"); err != nil {
		return fmt.Errorf("hunk header %q: %w", header, err)
	}
	return nil
}

func parseRange(s, sign string) (start, count int, err error) {
	s, ok := strings.CutPrefix(s, sign)
	if !ok {
		return 0, 0, fmt.Errorf("range %q missing %q", s, sign)
	}
	startStr, countStr, hasCount := strings.Cut(s, ",")
	if start, err = strconv.Atoi(startStr); err != nil {
		return 0, 0, err
	}
	count = 1
	if hasCount {
		if count, err = strconv.Atoi(countStr); err != nil {
			return 0, 0, err
		}
	}
	return start, count, nil
}
