package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// UpsertRepo returns the repo with rootSHA, creating it if needed, and records path as its last
// seen path. Nothing is written when the path is unchanged.
func (q queries) UpsertRepo(ctx context.Context, rootSHA, path string) (Repo, error) {
	repo, err := q.repoByRoot(ctx, rootSHA)
	switch {
	case err == nil:
		if repo.LastPath == path {
			return repo, nil
		}
		repo.LastPath, repo.UpdatedAt = path, q.now()
		_, err := q.q.ExecContext(ctx, `UPDATE repos SET last_path = ?, updated_at = ? WHERE id = ?`,
			repo.LastPath, formatTime(repo.UpdatedAt), repo.ID)
		return repo, err
	case !errors.Is(err, sql.ErrNoRows):
		return Repo{}, err
	}
	now := q.now()
	repo = Repo{ID: newID(), RootSHA: rootSHA, LastPath: path, CreatedAt: now, UpdatedAt: now}
	_, err = q.q.ExecContext(ctx,
		`INSERT INTO repos (id, root_sha, last_path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		repo.ID, repo.RootSHA, repo.LastPath, formatTime(now), formatTime(now))
	if err != nil {
		return Repo{}, fmt.Errorf("create repo: %w", err)
	}
	return repo, nil
}

func (q queries) repoByRoot(ctx context.Context, rootSHA string) (Repo, error) {
	var r Repo
	err := q.q.QueryRowContext(ctx,
		`SELECT id, root_sha, last_path, created_at, updated_at FROM repos WHERE root_sha = ?`, rootSHA,
	).Scan(&r.ID, &r.RootSHA, &r.LastPath, scanTime(&r.CreatedAt), scanTime(&r.UpdatedAt))
	return r, err
}

// RepoByRoot looks up a repo by its root commit.
func (q queries) RepoByRoot(ctx context.Context, rootSHA string) (Repo, error) {
	r, err := q.repoByRoot(ctx, rootSHA)
	return r, notFound(err, "repo "+rootSHA)
}

const changeColumns = `id, repo_id, seq, kind, branch, base_branch, title, state, created_at, updated_at`

func scanChange(row interface{ Scan(...any) error }) (Change, error) {
	var c Change
	err := row.Scan(&c.ID, &c.RepoID, &c.Seq, &c.Kind, &c.Branch, &c.BaseBranch, &c.Title, &c.State,
		scanTime(&c.CreatedAt), scanTime(&c.UpdatedAt))
	return c, err
}

// CreateChange inserts a change, assigning its id, seq, and timestamps.
func (q queries) CreateChange(ctx context.Context, c Change) (Change, error) {
	seq, err := q.nextSeq(ctx, "changes", "repo_id", c.RepoID)
	if err != nil {
		return Change{}, err
	}
	now := q.now()
	c.ID, c.Seq, c.CreatedAt, c.UpdatedAt = newID(), seq, now, now
	if c.State == "" {
		c.State = ChangeOpen
	}
	_, err = q.q.ExecContext(ctx, `INSERT INTO changes (`+changeColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.RepoID, c.Seq, c.Kind, c.Branch, c.BaseBranch, c.Title, c.State, formatTime(now), formatTime(now))
	if err != nil {
		return Change{}, fmt.Errorf("create change: %w", err)
	}
	return c, nil
}

// GetChange looks up a change by id.
func (q queries) GetChange(ctx context.Context, id string) (Change, error) {
	c, err := scanChange(q.q.QueryRowContext(ctx, `SELECT `+changeColumns+` FROM changes WHERE id = ?`, id))
	return c, notFound(err, "change "+id)
}

// ChangeBySeq looks up a change by its number within a repo.
func (q queries) ChangeBySeq(ctx context.Context, repoID string, seq int) (Change, error) {
	c, err := scanChange(q.q.QueryRowContext(ctx,
		`SELECT `+changeColumns+` FROM changes WHERE repo_id = ? AND seq = ?`, repoID, seq))
	return c, notFound(err, fmt.Sprintf("change %d", seq))
}

// ResolveChangePrefix finds the change in a repo whose id starts with prefix.
func (q queries) ResolveChangePrefix(ctx context.Context, repoID, prefix string) (Change, error) {
	id, err := q.resolvePrefix(ctx, "changes", "repo_id", repoID, prefix)
	if err != nil {
		return Change{}, fmt.Errorf("change %s: %w", prefix, err)
	}
	return q.GetChange(ctx, id)
}

// OpenBranchChange finds the open change for a branch.
func (q queries) OpenBranchChange(ctx context.Context, repoID, branch string) (Change, error) {
	c, err := scanChange(q.q.QueryRowContext(ctx,
		`SELECT `+changeColumns+` FROM changes WHERE repo_id = ? AND kind = 'branch' AND branch = ? AND state = 'open'`,
		repoID, branch))
	return c, notFound(err, "open change for branch "+branch)
}

// FrozenChange finds a range or commit change by its only revision's shas.
func (q queries) FrozenChange(ctx context.Context, repoID string, kind ChangeKind, baseSHA, headSHA string) (Change, error) {
	c, err := scanChange(q.q.QueryRowContext(ctx,
		`SELECT `+prefixed("c", changeColumns)+`
		FROM changes c JOIN revisions r ON r.change_id = c.id
		WHERE c.repo_id = ? AND c.kind = ? AND r.base_sha = ? AND r.head_sha = ?
		ORDER BY c.seq DESC LIMIT 1`,
		repoID, kind, baseSHA, headSHA))
	return c, notFound(err, fmt.Sprintf("%s change %s..%s", kind, baseSHA, headSHA))
}

// ListChanges lists a repo's changes, newest first. With no states, all are listed.
func (q queries) ListChanges(ctx context.Context, repoID string, states ...ChangeState) ([]Change, error) {
	query := `SELECT ` + changeColumns + ` FROM changes WHERE repo_id = ?`
	args := []any{repoID}
	if len(states) > 0 {
		query += ` AND state IN (` + placeholders(len(states)) + `)`
		for _, s := range states {
			args = append(args, s)
		}
	}
	query += ` ORDER BY seq DESC`
	rows, err := q.q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var changes []Change
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}

// SetChangeState moves a change through its lifecycle.
func (q queries) SetChangeState(ctx context.Context, id string, state ChangeState) error {
	return q.execOne(ctx, "change "+id,
		`UPDATE changes SET state = ?, updated_at = ? WHERE id = ?`, state, formatTime(q.now()), id)
}

// SetChangeTitle sets a change's title.
func (q queries) SetChangeTitle(ctx context.Context, id, title string) error {
	return q.execOne(ctx, "change "+id,
		`UPDATE changes SET title = ?, updated_at = ? WHERE id = ?`, title, formatTime(q.now()), id)
}

// SetChangeBase sets a branch change's base branch.
func (q queries) SetChangeBase(ctx context.Context, id, base string) error {
	return q.execOne(ctx, "change "+id,
		`UPDATE changes SET base_branch = ?, updated_at = ? WHERE id = ?`, base, formatTime(q.now()), id)
}

// CountChangeContent counts a change's reviews and threads, drafts included.
func (q queries) CountChangeContent(ctx context.Context, id string) (reviews, threads int, err error) {
	err = q.q.QueryRowContext(ctx,
		`SELECT (SELECT count(*) FROM reviews WHERE change_id = ?), (SELECT count(*) FROM threads WHERE change_id = ?)`,
		id, id).Scan(&reviews, &threads)
	return reviews, threads, err
}

// DeleteChange deletes a change with everything on it: its reviews, threads, comments, reviewed
// marks, and revisions. Reviews and threads point at revisions, so they go first.
func (q queries) DeleteChange(ctx context.Context, id string) error {
	for _, table := range []string{"reviewed", "threads", "reviews", "revisions"} {
		if _, err := q.q.ExecContext(ctx, `DELETE FROM `+table+` WHERE change_id = ?`, id); err != nil {
			return err
		}
	}
	return q.execOne(ctx, "change "+id, `DELETE FROM changes WHERE id = ?`, id)
}

const revisionColumns = `id, change_id, seq, base_sha, head_sha, created_at`

func scanRevision(row interface{ Scan(...any) error }) (Revision, error) {
	var r Revision
	err := row.Scan(&r.ID, &r.ChangeID, &r.Seq, &r.BaseSHA, &r.HeadSHA, scanTime(&r.CreatedAt))
	return r, err
}

// CreateRevision records a new revision of a change.
func (q queries) CreateRevision(ctx context.Context, changeID, baseSHA, headSHA string) (Revision, error) {
	seq, err := q.nextSeq(ctx, "revisions", "change_id", changeID)
	if err != nil {
		return Revision{}, err
	}
	r := Revision{ID: newID(), ChangeID: changeID, Seq: seq, BaseSHA: baseSHA, HeadSHA: headSHA, CreatedAt: q.now()}
	_, err = q.q.ExecContext(ctx, `INSERT INTO revisions (`+revisionColumns+`) VALUES (?, ?, ?, ?, ?, ?)`,
		r.ID, r.ChangeID, r.Seq, r.BaseSHA, r.HeadSHA, formatTime(r.CreatedAt))
	if err != nil {
		return Revision{}, fmt.Errorf("create revision: %w", err)
	}
	return r, nil
}

// GetRevision looks up a revision by id.
func (q queries) GetRevision(ctx context.Context, id string) (Revision, error) {
	r, err := scanRevision(q.q.QueryRowContext(ctx, `SELECT `+revisionColumns+` FROM revisions WHERE id = ?`, id))
	return r, notFound(err, "revision "+id)
}

// RevisionBySeq looks up a revision by its number within a change.
func (q queries) RevisionBySeq(ctx context.Context, changeID string, seq int) (Revision, error) {
	r, err := scanRevision(q.q.QueryRowContext(ctx,
		`SELECT `+revisionColumns+` FROM revisions WHERE change_id = ? AND seq = ?`, changeID, seq))
	return r, notFound(err, fmt.Sprintf("revision %d", seq))
}

// LatestRevision returns a change's newest revision.
func (q queries) LatestRevision(ctx context.Context, changeID string) (Revision, error) {
	r, err := scanRevision(q.q.QueryRowContext(ctx,
		`SELECT `+revisionColumns+` FROM revisions WHERE change_id = ? ORDER BY seq DESC LIMIT 1`, changeID))
	return r, notFound(err, "revision of change "+changeID)
}

// ListRevisions lists a change's revisions, oldest first.
func (q queries) ListRevisions(ctx context.Context, changeID string) ([]Revision, error) {
	rows, err := q.q.QueryContext(ctx,
		`SELECT `+revisionColumns+` FROM revisions WHERE change_id = ? ORDER BY seq`, changeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var revisions []Revision
	for rows.Next() {
		r, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, r)
	}
	return revisions, rows.Err()
}

// prefixed qualifies each column in a comma separated list with a table alias.
func prefixed(alias, columns string) string {
	var b strings.Builder
	for i, col := range strings.Split(columns, ",") {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(alias + "." + strings.TrimSpace(col))
	}
	return b.String()
}

func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	b := make([]byte, 0, 2*n)
	for i := range n {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

// execOne runs an update that must affect exactly one row.
func (q queries) execOne(ctx context.Context, what, query string, args ...any) error {
	res, err := q.q.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return nil
}
