package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenWritesAndRotates(t *testing.T) {
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

	// Past maxSize, the file moves aside on the next open.
	if err := os.WriteFile(path, make([]byte, maxSize+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, closer, err = Open(path, slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	closer.Close()
	if info, err := os.Stat(path + ".1"); err != nil || info.Size() != maxSize+1 {
		t.Errorf("rotated copy: %v, %v", info, err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 0 {
		t.Errorf("new log: %v, %v", info, err)
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
