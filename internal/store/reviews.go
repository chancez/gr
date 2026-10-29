package store

import (
	"context"
	"fmt"
)

const reviewColumns = `id, change_id, revision_id, seq, author_name, author_email, state, verdict, body, created_at, updated_at, submitted_at`

func scanReview(row interface{ Scan(...any) error }) (Review, error) {
	var r Review
	err := row.Scan(&r.ID, &r.ChangeID, &r.RevisionID, &r.Seq, &r.Author.Name, &r.Author.Email, &r.State,
		&r.Verdict, &r.Body, scanTime(&r.CreatedAt), scanTime(&r.UpdatedAt), scanTime(&r.SubmittedAt))
	return r, err
}

// CreateReview starts a draft review.
func (q queries) CreateReview(ctx context.Context, changeID, revisionID string, author Author) (Review, error) {
	seq, err := q.nextSeq(ctx, "reviews", "change_id", changeID)
	if err != nil {
		return Review{}, err
	}
	now := q.now()
	r := Review{
		ID: newID(), ChangeID: changeID, RevisionID: revisionID, Seq: seq, Author: author,
		State: ReviewDraft, CreatedAt: now, UpdatedAt: now,
	}
	_, err = q.q.ExecContext(ctx, `INSERT INTO reviews (`+reviewColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ChangeID, r.RevisionID, r.Seq, r.Author.Name, r.Author.Email, r.State, r.Verdict, r.Body,
		formatTime(now), formatTime(now), nil)
	if err != nil {
		return Review{}, fmt.Errorf("create review: %w", err)
	}
	return r, nil
}

// GetReview looks up a review by id.
func (q queries) GetReview(ctx context.Context, id string) (Review, error) {
	r, err := scanReview(q.q.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM reviews WHERE id = ?`, id))
	return r, notFound(err, "review "+id)
}

// ReviewBySeq looks up a review by its number within a change.
func (q queries) ReviewBySeq(ctx context.Context, changeID string, seq int) (Review, error) {
	r, err := scanReview(q.q.QueryRowContext(ctx,
		`SELECT `+reviewColumns+` FROM reviews WHERE change_id = ? AND seq = ?`, changeID, seq))
	return r, notFound(err, fmt.Sprintf("review %d", seq))
}

// DraftReview returns an author's draft review on a change.
func (q queries) DraftReview(ctx context.Context, changeID string, author Author) (Review, error) {
	r, err := scanReview(q.q.QueryRowContext(ctx,
		`SELECT `+reviewColumns+` FROM reviews WHERE change_id = ? AND author_name = ? AND author_email = ? AND state = 'draft'`,
		changeID, author.Name, author.Email))
	return r, notFound(err, "draft review by "+author.String())
}

// ListReviews lists a change's reviews in order. Drafts are included only for viewer.
func (q queries) ListReviews(ctx context.Context, changeID string, viewer Author) ([]Review, error) {
	rows, err := q.q.QueryContext(ctx,
		`SELECT `+reviewColumns+` FROM reviews
		WHERE change_id = ? AND (state = 'submitted' OR (author_name = ? AND author_email = ?))
		ORDER BY seq`,
		changeID, viewer.Name, viewer.Email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reviews []Review
	for rows.Next() {
		r, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		reviews = append(reviews, r)
	}
	return reviews, rows.Err()
}

// SetReviewBody replaces a draft review's summary.
func (q queries) SetReviewBody(ctx context.Context, id, body string) error {
	return q.execOne(ctx, "draft review "+id,
		`UPDATE reviews SET body = ?, updated_at = ? WHERE id = ? AND state = 'draft'`, body, formatTime(q.now()), id)
}

// SubmitReview publishes a draft review against a revision.
func (q queries) SubmitReview(ctx context.Context, id, revisionID string, verdict Verdict, body string) error {
	now := formatTime(q.now())
	return q.execOne(ctx, "draft review "+id,
		`UPDATE reviews SET state = 'submitted', revision_id = ?, verdict = ?, body = ?, updated_at = ?, submitted_at = ?
		WHERE id = ? AND state = 'draft'`,
		revisionID, verdict, body, now, now, id)
}

// DeleteReview discards a draft review with its threads and comments.
func (q queries) DeleteReview(ctx context.Context, id string) error {
	return q.execOne(ctx, "draft review "+id, `DELETE FROM reviews WHERE id = ? AND state = 'draft'`, id)
}
