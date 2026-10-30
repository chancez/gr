package store

import (
	"context"
	"fmt"
)

// AddMark records that an author reviewed something. Marking it again does nothing.
func (q queries) AddMark(ctx context.Context, changeID string, author Author, m Mark) error {
	_, err := q.q.ExecContext(ctx,
		`INSERT INTO reviewed (id, change_id, author_name, author_email, commit_sha, path, blob_sha, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		newID(), changeID, author.Name, author.Email, m.Commit, m.Path, m.Blob, formatTime(q.now()))
	if err != nil {
		return fmt.Errorf("mark reviewed: %w", err)
	}
	return nil
}

// DeleteMark removes a reviewed mark, if it exists.
func (q queries) DeleteMark(ctx context.Context, changeID string, author Author, m Mark) error {
	_, err := q.q.ExecContext(ctx,
		`DELETE FROM reviewed WHERE change_id = ? AND author_name = ? AND author_email = ?
		AND commit_sha = ? AND path = ? AND blob_sha = ?`,
		changeID, author.Name, author.Email, m.Commit, m.Path, m.Blob)
	return err
}

// ListMarks lists an author's reviewed marks on a change.
func (q queries) ListMarks(ctx context.Context, changeID string, author Author) ([]Mark, error) {
	rows, err := q.q.QueryContext(ctx,
		`SELECT commit_sha, path, blob_sha FROM reviewed WHERE change_id = ? AND author_name = ? AND author_email = ?
		ORDER BY created_at, id`,
		changeID, author.Name, author.Email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var marks []Mark
	for rows.Next() {
		var m Mark
		if err := rows.Scan(&m.Commit, &m.Path, &m.Blob); err != nil {
			return nil, err
		}
		marks = append(marks, m)
	}
	return marks, rows.Err()
}
