package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestMigrateFromFirstVersion opens a database made by the first schema and checks that later
// migrations apply over it.
func TestMigrateFromFirstVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gr.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(migrations[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != len(migrations) {
		t.Errorf("schema version = %d, want %d", version, len(migrations))
	}
	if _, err := s.ListMarks(t.Context(), "none", Author{}); err != nil {
		t.Errorf("reviewed table missing after migrating: %v", err)
	}
}
