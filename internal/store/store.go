// Package store persists reviews in SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a looked up row does not exist.
var ErrNotFound = errors.New("not found")

// ErrAmbiguous is returned when an id prefix matches more than one row.
var ErrAmbiguous = errors.New("ambiguous id prefix")

// Store is a handle on the database.
type Store struct {
	queries
	db   *sql.DB
	path string
}

// Option configures a Store.
type Option func(*Store)

// WithClock replaces the clock, so tests can assert exact timestamps.
func WithClock(now func() time.Time) Option {
	return func(s *Store) { s.now = now }
}

// DefaultPath is $XDG_DATA_HOME/gr/gr.db, defaulting to ~/.local/share/gr/gr.db.
func DefaultPath() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "gr", "gr.db"), nil
}

// Open opens or creates the database at path and brings its schema up to date.
func Open(ctx context.Context, path string, opts ...Option) (*Store, error) {
	// The driver splits its DSN at the first '?'.
	if strings.ContainsRune(path, '?') {
		return nil, fmt.Errorf("database path must not contain '?': %q", path)
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	dsn := path + "?" + strings.Join([]string{
		// WAL so the TUI and agents can read while another process writes.
		"_pragma=journal_mode(wal)",
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(on)",
		// Take the write lock at BEGIN. A deferred transaction upgrading from read to write can fail
		// with SQLITE_BUSY regardless of busy_timeout.
		"_txlock=immediate",
	}, "&")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	s := &Store{db: db, path: path}
	s.queries = queries{q: db, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Path is the database file.
func (s *Store) Path() string { return s.path }

func ensureDir(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	return nil
}

// migrate applies pending migrations. A database already up to date is not written to, because a
// write bumps data_version and wakes every TUI watching it.
func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current == len(migrations) {
		return nil
	}
	// Re-read inside the transaction: two processes can race on a first run.
	return s.Tx(ctx, func(tx *Tx) error {
		var version int
		if err := tx.q.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
			return fmt.Errorf("read schema version: %w", err)
		}
		if version > len(migrations) {
			return fmt.Errorf("database %s has schema version %d, newer than this gr understands (%d)", s.path, version, len(migrations))
		}
		for i := version; i < len(migrations); i++ {
			if _, err := tx.q.ExecContext(ctx, migrations[i]); err != nil {
				return fmt.Errorf("apply migration %d: %w", i+1, err)
			}
			if backfill, ok := backfills[i]; ok {
				if err := backfill(ctx, tx); err != nil {
					return fmt.Errorf("backfill migration %d: %w", i+1, err)
				}
			}
		}
		// PRAGMA takes no bound parameters.
		if _, err := tx.q.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations))); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
		return nil
	})
}

// Tx is a transaction. It has the same queries as Store.
type Tx struct {
	queries
}

// Tx runs fn in a transaction, committing if it returns nil.
func (s *Store) Tx(ctx context.Context, fn func(*Tx) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer sqlTx.Rollback()
	if err := fn(&Tx{queries{q: sqlTx, now: s.now}}); err != nil {
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// DataVersion opens a dedicated connection that reports when other connections commit.
//
// It must be its own connection: data_version only changes for commits made by other connections,
// so a pooled connection that did the write itself would see no change.
func (s *Store) DataVersion(ctx context.Context) (*Watcher, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	return &Watcher{conn: conn}, nil
}

// Watcher reads PRAGMA data_version on a dedicated connection.
type Watcher struct {
	conn *sql.Conn
}

// Version returns the current data version.
func (w *Watcher) Version(ctx context.Context) (int64, error) {
	var v int64
	if err := w.conn.QueryRowContext(ctx, `PRAGMA data_version`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read data_version: %w", err)
	}
	return v, nil
}

// Close releases the connection.
func (w *Watcher) Close() error { return w.conn.Close() }

type queryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// queries holds every query, so Store and Tx share them.
type queries struct {
	q   queryer
	now func() time.Time
}

func newID() string { return uuid.NewV7().String() }

const timeFormat = time.RFC3339Nano

func formatTime(t time.Time) string { return t.UTC().Format(timeFormat) }

func formatNullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return formatTime(t)
}

// timeValue scans a TEXT timestamp, NULL becoming the zero time.
type timeValue struct{ t *time.Time }

func (v timeValue) Scan(src any) error {
	switch s := src.(type) {
	case nil:
		*v.t = time.Time{}
		return nil
	case string:
		t, err := time.Parse(timeFormat, s)
		if err != nil {
			return err
		}
		*v.t = t
		return nil
	default:
		return fmt.Errorf("cannot scan %T into a time", src)
	}
}

func scanTime(t *time.Time) timeValue { return timeValue{t} }

// nextSeq allocates the next sequence number in a table, scoped by one column. Callers run it in a
// transaction, which holds the write lock from BEGIN.
func (q queries) nextSeq(ctx context.Context, table, scopeColumn, scope string) (int, error) {
	var seq int
	err := q.q.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT coalesce(max(seq), 0) + 1 FROM %s WHERE %s = ?`, table, scopeColumn), scope,
	).Scan(&seq)
	if err != nil {
		return 0, fmt.Errorf("allocate %s seq: %w", table, err)
	}
	return seq, nil
}

// resolvePrefix finds the one id in table starting with prefix, within a scope.
func (q queries) resolvePrefix(ctx context.Context, table, scopeColumn, scope, prefix string) (string, error) {
	rows, err := q.q.QueryContext(ctx,
		fmt.Sprintf(`SELECT id FROM %s WHERE %s = ? AND id LIKE ? ESCAPE '\' LIMIT 2`, table, scopeColumn),
		scope, escapeLike(prefix)+"%")
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(ids) {
	case 0:
		return "", ErrNotFound
	case 1:
		return ids[0], nil
	default:
		return "", ErrAmbiguous
	}
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return err
}
