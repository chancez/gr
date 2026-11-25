package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	got, err := Load(write(t, `
[ui]
theme = "colorblind"
glyphs = "nerd"
scrolloff = 0
sidebar_width = 30
auto_advance = true

[diff]
ignore_whitespace = true

[editor]
vim = true

[git]
timeout = "30s"
`))
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		UI:     UI{Theme: "colorblind", Glyphs: "nerd", Scrolloff: new(0), SidebarWidth: 30, AutoAdvance: true},
		Diff:   Diff{IgnoreWhitespace: true},
		Editor: Editor{Vim: true},
		Git:    Git{Timeout: new(Duration(30 * time.Second))},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
}

// No file is no settings, and an empty one too.
func TestLoadNothing(t *testing.T) {
	for _, path := range []string{filepath.Join(t.TempDir(), "missing.toml"), write(t, "")} {
		got, err := Load(path)
		if err != nil || !reflect.DeepEqual(got, Config{}) {
			t.Errorf("Load(%s) = %+v, %v; want no settings", path, got, err)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	for body, want := range map[string]string{
		"[ui]\nthme = \"x\"\n":        "unknown settings ui.thme",
		"[ui]\nscrolloff = -1\n":      "ui.scrolloff is -1",
		"[ui]\nsidebar_width = -2\n":  "ui.sidebar_width is -2",
		"[git]\ntimeout = \"-1s\"\n":  "git.timeout is -1s",
		"[git]\ntimeout = \"soon\"\n": "invalid duration",
		"[ui\n":                       "config ",
	} {
		_, err := Load(write(t, body))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Load(%q) = %v, want an error with %q", body, err, want)
		}
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("GR_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got, _ := DefaultPath(); got != "/xdg/gr/config.toml" {
		t.Errorf("DefaultPath = %q with XDG_CONFIG_HOME", got)
	}
	t.Setenv("GR_CONFIG", "/elsewhere.toml")
	if got, _ := DefaultPath(); got != "/elsewhere.toml" {
		t.Errorf("DefaultPath = %q with GR_CONFIG", got)
	}
}
