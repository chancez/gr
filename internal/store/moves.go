package store

import (
	"context"
	"fmt"
)

const moveColumns = `id, change_id, kind, from_name, to_name, how, author_name, author_email, created_at`

// CreateMove records a move of a change's branch or base, now.
func (q queries) CreateMove(ctx context.Context, m Move) (Move, error) {
	m.ID, m.CreatedAt = newID(), q.now()
	_, err := q.q.ExecContext(ctx, `INSERT INTO moves (`+moveColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ChangeID, m.Kind, m.From, m.To, m.How, m.Author.Name, m.Author.Email, formatTime(m.CreatedAt))
	if err != nil {
		return Move{}, fmt.Errorf("create move: %w", err)
	}
	return m, nil
}

// ChangeByOldBranch finds the change that most recently moved off branch, as one renamed from it.
func (q queries) ChangeByOldBranch(ctx context.Context, repoID, branch string) (Change, error) {
	c, err := scanChange(q.q.QueryRowContext(ctx,
		`SELECT `+prefixed("c", changeColumns)+`
		FROM changes c JOIN moves m ON m.change_id = c.id
		WHERE c.repo_id = ? AND m.kind = 'branch' AND m.from_name = ?
		ORDER BY m.created_at DESC, m.id DESC LIMIT 1`,
		repoID, branch))
	return c, notFound(err, "change that followed branch "+branch)
}

// ListMoves lists a change's moves, oldest first.
func (q queries) ListMoves(ctx context.Context, changeID string) ([]Move, error) {
	rows, err := q.q.QueryContext(ctx,
		`SELECT `+moveColumns+` FROM moves WHERE change_id = ? ORDER BY created_at, id`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var moves []Move
	for rows.Next() {
		var m Move
		if err := rows.Scan(&m.ID, &m.ChangeID, &m.Kind, &m.From, &m.To, &m.How, &m.Author.Name, &m.Author.Email,
			scanTime(&m.CreatedAt)); err != nil {
			return nil, err
		}
		moves = append(moves, m)
	}
	return moves, rows.Err()
}
