package store

import (
	"context"
	"database/sql"
	"errors"
)

// SavePlace records where an author was in a change, replacing what was there.
func (q queries) SavePlace(ctx context.Context, changeID string, author Author, place []byte) error {
	_, err := q.q.ExecContext(ctx,
		`INSERT INTO places (change_id, author_name, author_email, place, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (change_id, author_name, author_email) DO UPDATE SET place = excluded.place,
		updated_at = excluded.updated_at`,
		changeID, author.Name, author.Email, string(place), formatTime(q.now()))
	return err
}

// Place is where an author was in a change, or ErrNotFound.
func (q queries) Place(ctx context.Context, changeID string, author Author) ([]byte, error) {
	var place string
	err := q.q.QueryRowContext(ctx,
		`SELECT place FROM places WHERE change_id = ? AND author_name = ? AND author_email = ?`,
		changeID, author.Name, author.Email).Scan(&place)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return []byte(place), err
}
