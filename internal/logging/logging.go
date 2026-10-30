// Package logging sets up gr's log file. The TUI owns the terminal, so logs go to a file under the
// XDG state directory, where they can be read after the fact.
package logging

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// maxSize is the size past which the log file is rotated to a single older copy when it is opened.
const maxSize = 10 << 20

// DefaultPath is $XDG_STATE_HOME/gr/gr.log, defaulting to ~/.local/state/gr/gr.log.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "gr", "gr.log"), nil
}

// ParseLevel reads a level name: debug, info, warn, or error. Empty is info.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q, want debug, info, warn, or error", s)
}

// Open appends to the log file at path, creating it and its directory, and returns a logger for it
// at level. A file past maxSize is first moved aside to path.1, replacing the older copy.
func Open(path string, level slog.Level) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, nil, err
		}
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level}))
	return logger.With("pid", os.Getpid()), f, nil
}

// Discard is a logger that writes nothing, for when the log file cannot be opened.
func Discard() *slog.Logger { return slog.New(slog.DiscardHandler) }
