package cli

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"time"

	"github.com/chancez/gr/internal/review"
	"github.com/chancez/gr/internal/store"
)

// The JSON output is a contract for agents and editors. Change it additively. These types are kept
// apart from the store's so a schema change does not change the output.

type authorJSON struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type revisionJSON struct {
	ID        string    `json:"id"`
	Seq       int       `json:"seq"`
	Base      string    `json:"base"`
	Head      string    `json:"head"`
	CreatedAt time.Time `json:"created_at"`
}

type changeJSON struct {
	ID         string        `json:"id"`
	Seq        int           `json:"seq"`
	Kind       string        `json:"kind"`
	Branch     string        `json:"branch,omitempty"`
	BaseBranch string        `json:"base_branch,omitempty"`
	Title      string        `json:"title"`
	State      string        `json:"state"`
	Revision   *revisionJSON `json:"revision,omitzero"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
}

type reviewJSON struct {
	ID          string     `json:"id"`
	Seq         int        `json:"seq"`
	Author      authorJSON `json:"author"`
	State       string     `json:"state"`
	Verdict     string     `json:"verdict,omitempty"`
	Body        string     `json:"body,omitempty"`
	Revision    int        `json:"revision"`
	CreatedAt   time.Time  `json:"created_at"`
	SubmittedAt time.Time  `json:"submitted_at,omitzero"`
}

type locationJSON struct {
	Scope     string   `json:"scope"`
	Revision  int      `json:"revision"`
	Commit    string   `json:"commit,omitempty"`
	Path      string   `json:"path,omitempty"`
	Side      string   `json:"side,omitempty"`
	StartLine int      `json:"start_line,omitzero"`
	EndLine   int      `json:"end_line,omitzero"`
	Lines     []string `json:"lines,omitempty"`
}

type commentJSON struct {
	ID        string     `json:"id"`
	Author    authorJSON `json:"author"`
	Body      string     `json:"body"`
	Draft     bool       `json:"draft"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type lineJSON struct {
	Kind string `json:"kind"`
	Old  int    `json:"old,omitzero"`
	New  int    `json:"new,omitzero"`
	Text string `json:"text"`
}

// excerptJSON is the diff a thread is about: its anchored lines and a few before them.
type excerptJSON struct {
	Path string `json:"path"`
	Side string `json:"side"`
	Hunk string `json:"hunk,omitempty"`
	// Exact is false when the lines come from the anchor's stored content, as for outdated threads.
	Exact bool       `json:"exact"`
	Lines []lineJSON `json:"lines"`
}

type threadJSON struct {
	ID       string      `json:"id"`
	Seq      int         `json:"seq"`
	Change   int         `json:"change"`
	State    string      `json:"state"`
	StateBy  *authorJSON `json:"state_by,omitzero"`
	Outdated bool        `json:"outdated"`
	Draft    bool        `json:"draft"`
	// Review is the number of the review the thread was started in.
	Review int `json:"review"`
	// Summary is set on the thread holding a review's summary.
	Summary  bool          `json:"summary"`
	Location locationJSON  `json:"location"`
	Context  *excerptJSON  `json:"context,omitzero"`
	Comments []commentJSON `json:"comments"`
}

func toExcerpt(e review.Excerpt) *excerptJSON {
	out := &excerptJSON{Path: e.Path, Side: string(e.Side), Hunk: e.Hunk, Exact: e.Exact, Lines: []lineJSON{}}
	for _, l := range e.Lines {
		out.Lines = append(out.Lines, lineJSON{Kind: string(l.Kind), Old: l.OldLine, New: l.NewLine, Text: l.Text})
	}
	return out
}

func toAuthor(a store.Author) authorJSON { return authorJSON(a) }

func toRevision(r store.Revision) *revisionJSON {
	return &revisionJSON{ID: r.ID, Seq: r.Seq, Base: r.BaseSHA, Head: r.HeadSHA, CreatedAt: r.CreatedAt}
}

func toChange(c store.Change, rev *store.Revision) changeJSON {
	out := changeJSON{
		ID: c.ID, Seq: c.Seq, Kind: string(c.Kind), Branch: c.Branch, BaseBranch: c.BaseBranch, Title: c.Title,
		State: string(c.State), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	if rev != nil {
		out.Revision = toRevision(*rev)
	}
	return out
}

// lookups turns ids into the numbers people use, caching as it goes.
type lookups struct {
	svc       *review.Service
	revisions map[string]int
	changes   map[string]int
	reviews   map[string]store.Review
	// excerpts holds loaded thread excerpts by thread id. A loaded thread without lines has no entry.
	excerpts map[string]review.Excerpt
	loaded   map[string]bool
}

// loadExcerpts reads excerpts for many threads at once, sharing the diffs they are anchored to.
func (l *lookups) loadExcerpts(ctx context.Context, threads []review.ThreadView) error {
	excerpts, err := l.svc.Excerpts(ctx, threads)
	if err != nil {
		return err
	}
	for _, t := range threads {
		l.loaded[t.ID] = true
		if e, ok := excerpts[t.ID]; ok {
			l.excerpts[t.ID] = e
		}
	}
	return nil
}

func (l *lookups) excerpt(ctx context.Context, v review.ThreadView) (review.Excerpt, bool, error) {
	if !l.loaded[v.ID] {
		if err := l.loadExcerpts(ctx, []review.ThreadView{v}); err != nil {
			return review.Excerpt{}, false, err
		}
	}
	e, ok := l.excerpts[v.ID]
	return e, ok, nil
}

func newLookups(svc *review.Service) *lookups {
	return &lookups{
		svc: svc, revisions: map[string]int{}, changes: map[string]int{}, reviews: map[string]store.Review{},
		excerpts: map[string]review.Excerpt{}, loaded: map[string]bool{},
	}
}

func (l *lookups) revision(ctx context.Context, id string) (int, error) {
	if seq, ok := l.revisions[id]; ok {
		return seq, nil
	}
	r, err := l.svc.Store().GetRevision(ctx, id)
	if err != nil {
		return 0, err
	}
	l.revisions[id] = r.Seq
	return r.Seq, nil
}

func (l *lookups) change(ctx context.Context, id string) (int, error) {
	if seq, ok := l.changes[id]; ok {
		return seq, nil
	}
	c, err := l.svc.Store().GetChange(ctx, id)
	if err != nil {
		return 0, err
	}
	l.changes[id] = c.Seq
	return c.Seq, nil
}

func (l *lookups) reviewByID(ctx context.Context, reviewID string) (store.Review, error) {
	if r, ok := l.reviews[reviewID]; ok {
		return r, nil
	}
	r, err := l.svc.Store().GetReview(ctx, reviewID)
	if err != nil {
		return store.Review{}, err
	}
	l.reviews[reviewID] = r
	return r, nil
}

func (l *lookups) draft(ctx context.Context, reviewID string) (bool, error) {
	if reviewID == "" {
		return false, nil
	}
	r, err := l.reviewByID(ctx, reviewID)
	return r.State == store.ReviewDraft, err
}

func (l *lookups) review(ctx context.Context, r store.Review) (reviewJSON, error) {
	rev, err := l.revision(ctx, r.RevisionID)
	if err != nil {
		return reviewJSON{}, err
	}
	return reviewJSON{
		ID: r.ID, Seq: r.Seq, Author: toAuthor(r.Author), State: string(r.State), Verdict: string(r.Verdict),
		Body: r.Body, Revision: rev, CreatedAt: r.CreatedAt, SubmittedAt: r.SubmittedAt,
	}, nil
}

func (l *lookups) thread(ctx context.Context, v review.ThreadView) (threadJSON, error) {
	changeSeq, err := l.change(ctx, v.ChangeID)
	if err != nil {
		return threadJSON{}, err
	}
	revSeq, err := l.revision(ctx, v.Location.RevisionID)
	if err != nil {
		return threadJSON{}, err
	}
	draft, err := l.draft(ctx, v.ReviewID)
	if err != nil {
		return threadJSON{}, err
	}
	started, err := l.reviewByID(ctx, v.ReviewID)
	if err != nil {
		return threadJSON{}, err
	}
	loc := v.Location
	out := threadJSON{
		ID: v.ID, Seq: v.Seq, Change: changeSeq, State: string(v.State), Outdated: v.Outdated, Draft: draft,
		Review: started.Seq, Summary: v.Summary,
		Location: locationJSON{
			Scope: string(loc.Scope), Revision: revSeq, Commit: loc.Commit, Path: loc.Path, Side: string(loc.Side),
			StartLine: loc.StartLine, EndLine: loc.EndLine, Lines: loc.Lines,
		},
		Comments: []commentJSON{},
	}
	if v.StateBy.Name != "" {
		out.StateBy = new(toAuthor(v.StateBy))
	}
	if e, ok, err := l.excerpt(ctx, v); err != nil {
		return threadJSON{}, err
	} else if ok {
		out.Context = toExcerpt(e)
	}
	for _, c := range v.Comments {
		draft, err := l.draft(ctx, c.ReviewID)
		if err != nil {
			return threadJSON{}, err
		}
		out.Comments = append(out.Comments, commentJSON{
			ID: c.ID, Author: toAuthor(c.Author), Body: c.Body, Draft: draft, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		})
	}
	return out, nil
}

func (a *app) printJSON(v any) error {
	if err := json.MarshalWrite(a.stdout, v, jsontext.WithIndent("  ")); err != nil {
		return err
	}
	_, err := a.stdout.Write([]byte("\n"))
	return err
}
