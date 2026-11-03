package store

import (
	"strconv"
	"time"

	"github.com/chancez/gr/internal/anchor"
)

// Author is a human or a named agent.
type Author struct {
	Name  string
	Email string
}

func (a Author) String() string {
	if a.Email == "" {
		return a.Name
	}
	return a.Name + " <" + a.Email + ">"
}

// Repo is a git repository, identified by its root commit.
type Repo struct {
	ID        string
	RootSHA   string
	LastPath  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ChangeKind is what a change targets.
type ChangeKind string

const (
	KindBranch ChangeKind = "branch"
	KindRange  ChangeKind = "range"
	KindCommit ChangeKind = "commit"
)

// ChangeState is where a change is in its lifecycle.
type ChangeState string

const (
	ChangeOpen      ChangeState = "open"
	ChangeMerged    ChangeState = "merged"
	ChangeAbandoned ChangeState = "abandoned"
)

// Change is the thing under review.
type Change struct {
	ID     string
	RepoID string
	Seq    int
	Kind   ChangeKind
	// Branch and BaseBranch are set for branch changes.
	Branch     string
	BaseBranch string
	Title      string
	State      ChangeState
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Revision is a snapshot of a change.
type Revision struct {
	ID       string
	ChangeID string
	Seq      int
	// BaseSHA is empty when the change starts at a root commit.
	BaseSHA   string
	HeadSHA   string
	CreatedAt time.Time
}

// ReviewState is whether a review is visible to others.
type ReviewState string

const (
	ReviewDraft     ReviewState = "draft"
	ReviewSubmitted ReviewState = "submitted"
)

// Verdict is what a submitted review concludes.
type Verdict string

const (
	VerdictNone           Verdict = ""
	VerdictComment        Verdict = "comment"
	VerdictApprove        Verdict = "approve"
	VerdictRequestChanges Verdict = "request_changes"
)

// Review is one author's batch of threads and comments on a change.
type Review struct {
	ID       string
	ChangeID string
	// RevisionID is the revision the review was started on, or submitted against once submitted.
	RevisionID  string
	Seq         int
	Author      Author
	State       ReviewState
	Verdict     Verdict
	Body        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	SubmittedAt time.Time
}

// ThreadState is how far a thread is from done.
type ThreadState string

const (
	ThreadOpen      ThreadState = "open"
	ThreadAddressed ThreadState = "addressed"
	ThreadResolved  ThreadState = "resolved"
)

// Scope is what a thread is attached to.
type Scope string

const (
	// ScopeChange is a top-level thread on the whole change.
	ScopeChange Scope = "change"
	// ScopeCommit is attached to one commit, and optionally a file and lines in it.
	ScopeCommit Scope = "commit"
	// ScopeDiff is attached to the change's whole diff, and optionally a file and lines in it.
	ScopeDiff Scope = "diff"
)

// Location is where a thread points. An empty Path means the whole commit or change. A zero
// StartLine means the whole file.
type Location struct {
	Scope Scope
	// RevisionID is the revision whose commits or diff the location refers to.
	RevisionID string
	// Commit is set for ScopeCommit.
	Commit string
	// BlobSHA is the file's blob on the anchored side, if it exists there.
	BlobSHA string
	anchor.Anchor
}

// Thread is a conversation with an optional location.
type Thread struct {
	ID       string
	ChangeID string
	Seq      int
	// RevisionID is the revision the thread was created on.
	RevisionID string
	// ReviewID is the review the thread was started in.
	ReviewID string
	State    ThreadState
	// StateBy and StateAt record who last changed State, and when.
	StateBy  Author
	StateAt  time.Time
	Outdated bool
	// Summary marks the thread holding its review's summary.
	Summary   bool
	Location  Location
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Comment is one message in a thread.
type Comment struct {
	ID       string
	ThreadID string
	// ReviewID is empty for a reply published outside a review.
	ReviewID  string
	Author    Author
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Mark identifies something an author marked reviewed: a commit (Commit only), a file in a commit
// (Commit and Path), or a file's content in the change's whole diff (Path and Blob).
type Mark struct {
	Commit string
	Path   string
	Blob   string
}

// AuthorMark is a mark and the author who made it.
type AuthorMark struct {
	Author Author
	Mark   Mark
}

// Ref is how a thread is referred to: its number, or for a summary thread "r" and its review's
// number.
func (t Thread) Ref() string {
	if t.Summary {
		return "r" + strconv.Itoa(-t.Seq)
	}
	return strconv.Itoa(t.Seq)
}

// Label is Ref as shown to people: "#5" or "r1".
func (t Thread) Label() string {
	if t.Summary {
		return t.Ref()
	}
	return "#" + t.Ref()
}
