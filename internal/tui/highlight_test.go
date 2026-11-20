package tui

import (
	"testing"

	"github.com/chancez/gr/internal/git"
)

func TestInterpreter(t *testing.T) {
	for line, want := range map[string]string{
		"#!/bin/sh":                "sh",
		"#! /bin/bash -e":          "bash",
		"#!/usr/bin/env python3":   "python",
		"#!/usr/bin/python3.12":    "python",
		"#!/usr/bin/env -S uv run": "uv",
		"#!/usr/bin/env A=1 node":  "node",
		"#!/usr/bin/env":           "",
		"#!":                       "",
		"# not a shebang":          "",
		"package main":             "",
	} {
		if got := interpreter(line); got != want {
			t.Errorf("interpreter(%q) = %q, want %q", line, got, want)
		}
	}
}

func TestLexerFromShebang(t *testing.T) {
	file := func(path, first string, firstLine int) git.FileDiff {
		return git.FileDiff{
			NewPath: path,
			Hunks: []git.Hunk{{
				Lines: []git.Line{{
					Kind:    git.LineContext,
					Text:    first,
					OldLine: firstLine,
					NewLine: firstLine,
				}},
			}},
		}
	}
	for _, tc := range []struct {
		name string
		f    git.FileDiff
		want string
	}{
		{"shebang", file("bin/run", "#!/bin/sh", 1), "Bash"},
		{"env", file("bin/run", "#!/usr/bin/env python3", 1), "Python"},
		{"alias", file("bin/run", "#!/usr/bin/env node", 1), "JavaScript"},
		{"name wins", file("main.go", "#!/bin/sh", 1), "Go"},
		{"not line one", file("bin/run", "#!/bin/sh", 2), ""},
		{"read first line", git.FileDiff{NewPath: "bin/run", FirstLine: "#!/bin/sh"}, "Bash"},
		{"no shebang", file("bin/run", "echo hi", 1), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ""
			if l := newHighlighter(true).lexer(tc.f); l != nil {
				got = l.Config().Name
			}
			if got != tc.want {
				t.Errorf("lexer = %q, want %q", got, tc.want)
			}
		})
	}
}
