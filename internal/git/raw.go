package git

import (
	"bytes"
	"context"
	"fmt"
	"strings"
)

// parseRaw parses the -z form of git's --raw output, ":mode mode blob blob status\0path\0" with a
// second path for renames and copies, into file diffs without hunks.
func parseRaw(out []byte) ([]FileDiff, error) {
	tokens := strings.Split(string(out), "\x00")
	var files []FileDiff
	for i := 0; i < len(tokens); i++ {
		tok := strings.TrimLeft(tokens[i], "\n")
		if tok == "" {
			continue
		}
		if !strings.HasPrefix(tok, ":") {
			return nil, fmt.Errorf("unexpected raw diff field %q", tok)
		}
		fields := strings.Fields(tok[1:])
		if len(fields) != 5 {
			return nil, fmt.Errorf("malformed raw diff entry %q", tok)
		}
		f := FileDiff{
			OldMode: nonZero(fields[0]), NewMode: nonZero(fields[1]),
			OldBlob: nonZero(fields[2]), NewBlob: nonZero(fields[3]),
		}
		status := fields[4]
		paths := 1
		switch status[0] {
		case 'A':
			f.Status = StatusAdded
		case 'D':
			f.Status = StatusDeleted
		case 'R':
			f.Status, paths = StatusRenamed, 2
		case 'C':
			f.Status, paths = StatusCopied, 2
		default:
			f.Status = StatusModified
		}
		if i+paths >= len(tokens) {
			return nil, fmt.Errorf("raw diff entry %q is missing its path", tok)
		}
		old, new := tokens[i+1], tokens[i+paths]
		i += paths
		switch f.Status {
		case StatusAdded:
			f.NewPath = new
		case StatusDeleted:
			f.OldPath = old
		default:
			f.OldPath, f.NewPath = old, new
		}
		files = append(files, f)
	}
	return files, nil
}

// DiffFiles lists the files that differ between two revisions, without their hunks. An empty from
// compares with the empty tree.
func (r *Repo) DiffFiles(ctx context.Context, from, to string) ([]FileDiff, error) {
	if from == "" {
		var err error
		if from, err = r.EmptyTree(ctx); err != nil {
			return nil, err
		}
	}
	out, err := r.run(ctx, "diff", "--no-ext-diff", "--no-relative", "--no-abbrev", "--raw", "-z", "-M", from, to, "--")
	if err != nil {
		return nil, err
	}
	return parseRaw(out)
}

// CommitFiles lists the files each commit in base..head changes against its first parent, without
// their hunks, in one git process. An empty base lists all of head's history.
func (r *Repo) CommitFiles(ctx context.Context, base, head string) (map[string][]FileDiff, error) {
	rng := head
	if base != "" {
		rng = base + ".." + head
	}
	out, err := r.run(ctx, "log", "--no-abbrev", "-M", "--raw", "-z", "--first-parent", "--root",
		"--format=%x1e%H", rng, "--")
	if err != nil {
		return nil, err
	}
	commits := map[string][]FileDiff{}
	for record := range bytes.SplitSeq(out, []byte{0x1e}) {
		sha, rest, ok := bytes.Cut(record, []byte{0})
		if !ok || len(sha) == 0 {
			continue
		}
		files, err := parseRaw(rest)
		if err != nil {
			return nil, fmt.Errorf("commit %s: %w", sha, err)
		}
		commits[string(sha)] = files
	}
	return commits, nil
}
