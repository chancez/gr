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
	"sync"
)

// maxSize is the size past which the log file is rotated to a single older copy.
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
// at level. The file rotates to path.1, replacing the older copy, whenever a write would take it
// past maxSize, so a long session cannot grow it without bound.
func Open(path string, level slog.Level) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	w := &rotatingFile{path: path, max: maxSize}
	if err := w.open(); err != nil {
		return nil, nil, err
	}
	logger := slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
	return logger.With("pid", os.Getpid()), w, nil
}

// rotatingFile appends to a file and rotates it once a write would take it past max.
//
// Another gr process writing the same log keeps its own handle, so after a rotation it goes on
// writing to the rotated copy until it rotates or exits itself. Its lines still land in one of the
// two files.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func (w *rotatingFile) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, info.Size()
	return nil
}

func (w *rotatingFile) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size > 0 && w.size+int64(len(p)) > w.max {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *rotatingFile) rotate() error {
	if err := w.f.Close(); err != nil {
		return err
	}
	if err := os.Rename(w.path, w.path+".1"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return w.open()
}

func (w *rotatingFile) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

// Discard is a logger that writes nothing, for when the log file cannot be opened.
func Discard() *slog.Logger { return slog.New(slog.DiscardHandler) }
