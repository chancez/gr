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

// TestBackfillSummaryThreads migrates a database whose submitted review predates summary threads.
func TestBackfillSummaryThreads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gr.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range append(migrations[:2:2],
		`INSERT INTO repos VALUES ('r', 'root', '/x', 't', 't')`,
		`INSERT INTO changes VALUES ('c', 'r', 1, 'branch', 'b', 'main', 'b', 'open', 't', 't')`,
		`INSERT INTO revisions VALUES ('v', 'c', 1, 'base', 'head', 't')`,
		`INSERT INTO reviews VALUES ('rv', 'c', 'v', 1, 'Human', 'h@x', 'submitted', 'request_changes', 'fix things', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO reviews VALUES ('empty', 'c', 'v', 2, 'Other', '', 'submitted', 'approve', '', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`PRAGMA user_version = 2`,
	) {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	db.Close()

	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	threads, err := s.ListThreads(t.Context(), ThreadFilter{ChangeID: "c", AllDrafts: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 || !threads[0].Summary || threads[0].ReviewID != "rv" || threads[0].Location.Scope != ScopeChange {
		t.Fatalf("threads after backfill = %+v, want one summary thread for the review with a body", threads)
	}
	comments, err := s.ListComments(t.Context(), threads[0].ID, Author{})
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Body != "fix things" || comments[0].Author != (Author{Name: "Human", Email: "h@x"}) {
		t.Errorf("summary comments = %+v", comments)
	}
}
