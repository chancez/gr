// Package config reads gr's config file, TOML at $XDG_CONFIG_HOME/gr/config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the config file's settings. What the file leaves out is zero, for gr's defaults.
type Config struct {
	UI UI `toml:"ui"`
}

// UI is how the TUI looks and moves.
type UI struct {
	// Theme is a built-in palette: "default" or "colorblind". GR_THEME overrides it.
	Theme string `toml:"theme"`
	// Glyphs are the symbols drawn with: "ascii", "unicode", or "nerd". Empty picks them from the
	// terminal. GR_GLYPHS overrides it.
	Glyphs string `toml:"glyphs"`
	// Scrolloff is how many lines are kept between the cursor and the pane's edges, as in vim.
	Scrolloff *int `toml:"scrolloff"`
	// SidebarWidth is the sidebar's width in columns. Zero sizes it to the terminal.
	SidebarWidth int `toml:"sidebar_width"`
	// AutoAdvance moves on to the next unreviewed file after marking one reviewed, as + toggles.
	AutoAdvance bool `toml:"auto_advance"`
}

// DefaultPath is where the config file is: GR_CONFIG, or gr/config.toml in $XDG_CONFIG_HOME, by
// default ~/.config.
func DefaultPath() (string, error) {
	if p := os.Getenv("GR_CONFIG"); p != "" {
		return p, nil
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "gr", "config.toml"), nil
}

// Load reads the config file at path. No file is no settings. A key gr does not know is an error,
// so a misspelled setting is not silently ignored.
func Load(path string) (Config, error) {
	var c Config
	md, err := toml.DecodeFile(path, &c)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	if keys := md.Undecoded(); len(keys) > 0 {
		names := make([]string, len(keys))
		for i, k := range keys {
			names[i] = k.String()
		}
		return Config{}, fmt.Errorf("config %s: unknown settings %s", path, strings.Join(names, ", "))
	}
	if c.UI.Scrolloff != nil && *c.UI.Scrolloff < 0 {
		return Config{}, fmt.Errorf("config %s: ui.scrolloff is %d, want 0 or more", path, *c.UI.Scrolloff)
	}
	if c.UI.SidebarWidth < 0 {
		return Config{}, fmt.Errorf("config %s: ui.sidebar_width is %d, want 0 or more", path, c.UI.SidebarWidth)
	}
	return c, nil
}
