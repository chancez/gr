package store

import (
	"cmp"
	"context"
	"encoding/json/v2"
	"fmt"
	"strings"
)

const threadColumns = `id, change_id, seq, revision_id, review_id, state, state_by_name, state_by_email, state_at, outdated,
	scope, anchor_revision_id, commit_sha, path, blob_sha, side, start_line, end_line, lines, before_lines, after_lines,
	created_at, updated_at, summary`

// jsonLines stores a []string as a JSON array.
type jsonLines struct{ s *[]string }

func (v jsonLines) Scan(src any) error {
	text, ok := src.(string)
	if !ok {
		return fmt.Errorf("cannot scan %T into lines", src)
	}
	var lines []string
	if err := json.Unmarshal([]byte(text), &lines); err != nil {
		return err
	}
	if len(lines) == 0 {
		lines = nil
	}
	*v.s = lines
	return nil
}

func encodeLines(lines []string) (string, error) {
	if lines == nil {
		lines = []string{}
	}
	b, err := json.Marshal(lines)
	return string(b), err
}

func scanThread(row interface{ Scan(...any) error }) (Thread, error) {
	var t Thread
	l := &t.Location
	err := row.Scan(&t.ID, &t.ChangeID, &t.Seq, &t.RevisionID, &t.ReviewID, &t.State, &t.StateBy.Name,
		&t.StateBy.Email, scanTime(&t.StateAt), &t.Outdated,
		&l.Scope, &l.RevisionID, &l.Commit, &l.Path, &l.BlobSHA, &l.Side, &l.StartLine, &l.EndLine,
		jsonLines{&l.Lines}, jsonLines{&l.Before}, jsonLines{&l.After},
		scanTime(&t.CreatedAt), scanTime(&t.UpdatedAt), &t.Summary)
	return t, err
}

func locationArgs(l Location) ([]any, error) {
	lines, err := encodeLines(l.Lines)
	if err != nil {
		return nil, err
	}
	before, err := encodeLines(l.Before)
	if err != nil {
		return nil, err
	}
	after, err := encodeLines(l.After)
	if err != nil {
		return nil, err
	}
	return []any{l.Scope, l.RevisionID, l.Commit, l.Path, l.BlobSHA, l.Side, l.StartLine, l.EndLine, lines, before, after}, nil
}

// CreateThread inserts a thread, assigning its id, seq, and timestamps.
func (q queries) CreateThread(ctx context.Context, t Thread) (Thread, error) {
	seq, err := q.nextSeq(ctx, "threads", "change_id", t.ChangeID)
	if err != nil {
		return Thread{}, err
	}
	now := q.now()
	t.ID, t.Seq, t.CreatedAt, t.UpdatedAt = newID(), seq, now, now
	if t.State == "" {
		t.State = ThreadOpen
	}
	loc, err := locationArgs(t.Location)
	if err != nil {
		return Thread{}, err
	}
	args := append([]any{t.ID, t.ChangeID, t.Seq, t.RevisionID, t.ReviewID, t.State, t.StateBy.Name,
		t.StateBy.Email, formatNullTime(t.StateAt), t.Outdated}, loc...)
	args = append(args, formatTime(now), formatTime(now), t.Summary)
	_, err = q.q.ExecContext(ctx, `INSERT INTO threads (`+threadColumns+`) VALUES (`+placeholders(len(args))+`)`, args...)
	if err != nil {
		return Thread{}, fmt.Errorf("create thread: %w", err)
	}
	return t, nil
}

// CreateSummaryThread starts the thread holding a submitted review's summary, with the summary as
// its first comment.
func (q queries) CreateSummaryThread(ctx context.Context, r Review) (Thread, error) {
	t, err := q.CreateThread(ctx, Thread{
		ChangeID: r.ChangeID, RevisionID: r.RevisionID, ReviewID: r.ID, Summary: true,
		Location: Location{Scope: ScopeChange, RevisionID: r.RevisionID},
	})
	if err != nil {
		return Thread{}, err
	}
	if _, err := q.AddComment(ctx, Comment{ThreadID: t.ID, ReviewID: r.ID, Author: r.Author, Body: r.Body}); err != nil {
		return Thread{}, err
	}
	return t, nil
}

// GetThread looks up a thread by id.
func (q queries) GetThread(ctx context.Context, id string) (Thread, error) {
	t, err := scanThread(q.q.QueryRowContext(ctx, `SELECT `+threadColumns+` FROM threads WHERE id = ?`, id))
	return t, notFound(err, "thread "+id)
}

// ThreadBySeq looks up a thread by its number within a change.
func (q queries) ThreadBySeq(ctx context.Context, changeID string, seq int) (Thread, error) {
	t, err := scanThread(q.q.QueryRowContext(ctx,
		`SELECT `+threadColumns+` FROM threads WHERE change_id = ? AND seq = ?`, changeID, seq))
	return t, notFound(err, fmt.Sprintf("thread %d", seq))
}

// ResolveThreadPrefix finds the thread in a change whose id starts with prefix.
func (q queries) ResolveThreadPrefix(ctx context.Context, changeID, prefix string) (Thread, error) {
	id, err := q.resolvePrefix(ctx, "threads", "change_id", changeID, prefix)
	if err != nil {
		return Thread{}, fmt.Errorf("thread %s: %w", prefix, err)
	}
	return q.GetThread(ctx, id)
}

// ThreadFilter selects threads. Zero fields do not filter.
type ThreadFilter struct {
	ChangeID string
	// Commit selects threads anchored to a commit, across changes.
	Commit string
	States []ThreadState
	// Outdated, if set, selects threads by their outdated flag.
	Outdated *bool
	// Viewer sees their own draft threads. Other drafts are not listed unless AllDrafts is set.
	Viewer Author
	// AllDrafts lists every author's drafts, for maintenance such as re-anchoring.
	AllDrafts bool
}

// ListThreads lists threads ordered by change and seq.
func (q queries) ListThreads(ctx context.Context, f ThreadFilter) ([]Thread, error) {
	var where []string
	var args []any
	if !f.AllDrafts {
		where = append(where, `(r.state = 'submitted' OR (r.author_name = ? AND r.author_email = ?))`)
		args = append(args, f.Viewer.Name, f.Viewer.Email)
	}
	if f.ChangeID != "" {
		where = append(where, `t.change_id = ?`)
		args = append(args, f.ChangeID)
	}
	if f.Commit != "" {
		where = append(where, `t.commit_sha = ?`)
		args = append(args, f.Commit)
	}
	if len(f.States) > 0 {
		where = append(where, `t.state IN (`+placeholders(len(f.States))+`)`)
		for _, s := range f.States {
			args = append(args, s)
		}
	}
	if f.Outdated != nil {
		where = append(where, `t.outdated = ?`)
		args = append(args, *f.Outdated)
	}
	rows, err := q.q.QueryContext(ctx,
		`SELECT `+prefixed("t", threadColumns)+` FROM threads t JOIN reviews r ON r.id = t.review_id
		WHERE `+cmp.Or(strings.Join(where, " AND "), "1")+` ORDER BY t.change_id, t.seq`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var threads []Thread
	for rows.Next() {
		t, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		threads = append(threads, t)
	}
	return threads, rows.Err()
}

// MoveThread updates a thread's location and outdated flag after re-anchoring.
func (q queries) MoveThread(ctx context.Context, id string, l Location, outdated bool) error {
	loc, err := locationArgs(l)
	if err != nil {
		return err
	}
	args := append(loc, outdated, formatTime(q.now()), id)
	return q.execOne(ctx, "thread "+id,
		`UPDATE threads SET scope = ?, anchor_revision_id = ?, commit_sha = ?, path = ?, blob_sha = ?, side = ?,
		start_line = ?, end_line = ?, lines = ?, before_lines = ?, after_lines = ?, outdated = ?, updated_at = ?
		WHERE id = ?`, args...)
}

// SetThreadState records a thread's new state and who set it.
func (q queries) SetThreadState(ctx context.Context, id string, state ThreadState, by Author) error {
	now := formatTime(q.now())
	return q.execOne(ctx, "thread "+id,
		`UPDATE threads SET state = ?, state_by_name = ?, state_by_email = ?, state_at = ?, updated_at = ? WHERE id = ?`,
		state, by.Name, by.Email, now, now, id)
}

const commentColumns = `id, thread_id, review_id, author_name, author_email, body, created_at, updated_at`

func scanComment(row interface{ Scan(...any) error }) (Comment, error) {
	var c Comment
	var reviewID *string
	err := row.Scan(&c.ID, &c.ThreadID, &reviewID, &c.Author.Name, &c.Author.Email, &c.Body,
		scanTime(&c.CreatedAt), scanTime(&c.UpdatedAt))
	if reviewID != nil {
		c.ReviewID = *reviewID
	}
	return c, err
}

// AddComment inserts a comment, assigning its id and timestamps.
func (q queries) AddComment(ctx context.Context, c Comment) (Comment, error) {
	now := q.now()
	c.ID, c.CreatedAt, c.UpdatedAt = newID(), now, now
	var reviewID any
	if c.ReviewID != "" {
		reviewID = c.ReviewID
	}
	_, err := q.q.ExecContext(ctx, `INSERT INTO comments (`+commentColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ThreadID, reviewID, c.Author.Name, c.Author.Email, c.Body, formatTime(now), formatTime(now))
	if err != nil {
		return Comment{}, fmt.Errorf("add comment: %w", err)
	}
	return c, nil
}

// GetComment looks up a comment by id.
func (q queries) GetComment(ctx context.Context, id string) (Comment, error) {
	c, err := scanComment(q.q.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE id = ?`, id))
	return c, notFound(err, "comment "+id)
}

// ListComments lists a thread's comments in order. Comments in draft reviews are included only for
// viewer.
func (q queries) ListComments(ctx context.Context, threadID string, viewer Author) ([]Comment, error) {
	rows, err := q.q.QueryContext(ctx,
		`SELECT `+prefixed("c", commentColumns)+` FROM comments c LEFT JOIN reviews r ON r.id = c.review_id
		WHERE c.thread_id = ? AND (r.id IS NULL OR r.state = 'submitted' OR (r.author_name = ? AND r.author_email = ?))
		ORDER BY c.created_at, c.id`,
		threadID, viewer.Name, viewer.Email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comments []Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// EditComment replaces a comment's body.
func (q queries) EditComment(ctx context.Context, id, body string) error {
	return q.execOne(ctx, "comment "+id,
		`UPDATE comments SET body = ?, updated_at = ? WHERE id = ?`, body, formatTime(q.now()), id)
}

// DeleteComment removes a comment.
func (q queries) DeleteComment(ctx context.Context, id string) error {
	return q.execOne(ctx, "comment "+id, `DELETE FROM comments WHERE id = ?`, id)
}

// DeleteThread removes a thread and its comments.
func (q queries) DeleteThread(ctx context.Context, id string) error {
	return q.execOne(ctx, "thread "+id, `DELETE FROM threads WHERE id = ?`, id)
}
