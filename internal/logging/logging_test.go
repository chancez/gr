package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenWritesAtLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gr", "gr.log")
	logger, closer, err := Open(path, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	logger.Debug("hidden")
	logger.Info("shown", "k", "v")
	closer.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); strings.Contains(got, "hidden") || !strings.Contains(got, "msg=shown") || !strings.Contains(got, "k=v") {
		t.Errorf("log = %q", got)
	}

}

// TestRotatesDuringASession checks that the log rotates when a write would pass the limit, not
// only when a process opens it.
func TestRotatesDuringASession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gr.log")
	w := &rotatingFile{path: path, max: 100}
	if err := w.open(); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	line := []byte(strings.Repeat("x", 39) + "\n")
	for range 3 {
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	// Two lines fit in 100 bytes; the third rotates them out.
	old, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(old) != 80 || len(cur) != 40 {
		t.Errorf("after rotating: old %d bytes, current %d; want 80 and 40", len(old), len(cur))
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{"": slog.LevelInfo, "DEBUG": slog.LevelDebug, "warn": slog.LevelWarn, "error": slog.LevelError} {
		if got, err := ParseLevel(in); err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("ParseLevel accepted an unknown level")
	}
}
