package store

import (
	"context"
	"fmt"
)

const eventColumns = `id, change_id, kind, from_name, to_name, how, author_name, author_email, created_at`

// CreateEvent records what happened to a change, now.
func (q queries) CreateEvent(ctx context.Context, m Event) (Event, error) {
	m.ID, m.CreatedAt = newID(), q.now()
	_, err := q.q.ExecContext(ctx, `INSERT INTO events (`+eventColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ChangeID, m.Kind, m.From, m.To, m.How, m.Author.Name, m.Author.Email, formatTime(m.CreatedAt))
	if err != nil {
		return Event{}, fmt.Errorf("create event: %w", err)
	}
	return m, nil
}

// ChangeByOldBranch finds the change that most recently moved off branch, as one renamed from it.
func (q queries) ChangeByOldBranch(ctx context.Context, repoID, branch string) (Change, error) {
	c, err := scanChange(q.q.QueryRowContext(ctx,
		`SELECT `+prefixed("c", changeColumns)+`
		FROM changes c JOIN events m ON m.change_id = c.id
		WHERE c.repo_id = ? AND m.kind = 'branch' AND m.from_name = ?
		ORDER BY m.created_at DESC, m.id DESC LIMIT 1`,
		repoID, branch))
	return c, notFound(err, "change that followed branch "+branch)
}

// ListEvents lists what happened to a change, oldest first.
func (q queries) ListEvents(ctx context.Context, changeID string) ([]Event, error) {
	rows, err := q.q.QueryContext(ctx,
		`SELECT `+eventColumns+` FROM events WHERE change_id = ? ORDER BY created_at, id`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var m Event
		if err := rows.Scan(&m.ID, &m.ChangeID, &m.Kind, &m.From, &m.To, &m.How, &m.Author.Name, &m.Author.Email,
			scanTime(&m.CreatedAt)); err != nil {
			return nil, err
		}
		events = append(events, m)
	}
	return events, rows.Err()
}
