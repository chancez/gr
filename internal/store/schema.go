package store

// migrations build the schema one step at a time. The schema version, kept in PRAGMA user_version,
// is the number applied. Never edit a migration that has shipped: append a new one.
var migrations = []string{
	`
CREATE TABLE repos (
	id         TEXT PRIMARY KEY,
	root_sha   TEXT NOT NULL UNIQUE,
	last_path  TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE changes (
	id          TEXT PRIMARY KEY,
	repo_id     TEXT NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
	seq         INTEGER NOT NULL,
	kind        TEXT NOT NULL CHECK (kind IN ('branch', 'range', 'commit')),
	branch      TEXT NOT NULL DEFAULT '',
	base_branch TEXT NOT NULL DEFAULT '',
	title       TEXT NOT NULL DEFAULT '',
	state       TEXT NOT NULL CHECK (state IN ('open', 'merged', 'abandoned')),
	created_at  TEXT NOT NULL,
	updated_at  TEXT NOT NULL,
	UNIQUE (repo_id, seq)
);

-- A branch has at most one open change.
CREATE UNIQUE INDEX changes_open_branch ON changes (repo_id, branch) WHERE kind = 'branch' AND state = 'open';

CREATE TABLE revisions (
	id         TEXT PRIMARY KEY,
	change_id  TEXT NOT NULL REFERENCES changes(id) ON DELETE CASCADE,
	seq        INTEGER NOT NULL,
	base_sha   TEXT NOT NULL,
	head_sha   TEXT NOT NULL,
	created_at TEXT NOT NULL,
	UNIQUE (change_id, seq)
);

CREATE INDEX revisions_shas ON revisions (base_sha, head_sha);

CREATE TABLE reviews (
	id           TEXT PRIMARY KEY,
	change_id    TEXT NOT NULL REFERENCES changes(id) ON DELETE CASCADE,
	revision_id  TEXT NOT NULL REFERENCES revisions(id),
	seq          INTEGER NOT NULL,
	author_name  TEXT NOT NULL,
	author_email TEXT NOT NULL,
	state        TEXT NOT NULL CHECK (state IN ('draft', 'submitted')),
	verdict      TEXT NOT NULL DEFAULT '' CHECK (verdict IN ('', 'comment', 'approve', 'request_changes')),
	body         TEXT NOT NULL DEFAULT '',
	created_at   TEXT NOT NULL,
	updated_at   TEXT NOT NULL,
	submitted_at TEXT,
	UNIQUE (change_id, seq)
);

-- An author has at most one draft review per change.
CREATE UNIQUE INDEX reviews_one_draft ON reviews (change_id, author_name, author_email) WHERE state = 'draft';

CREATE TABLE threads (
	id                 TEXT PRIMARY KEY,
	change_id          TEXT NOT NULL REFERENCES changes(id) ON DELETE CASCADE,
	seq                INTEGER NOT NULL,
	revision_id        TEXT NOT NULL REFERENCES revisions(id),
	review_id          TEXT NOT NULL REFERENCES reviews(id) ON DELETE CASCADE,
	state              TEXT NOT NULL CHECK (state IN ('open', 'addressed', 'resolved')),
	state_by_name      TEXT NOT NULL DEFAULT '',
	state_by_email     TEXT NOT NULL DEFAULT '',
	state_at           TEXT,
	outdated           INTEGER NOT NULL DEFAULT 0,
	scope              TEXT NOT NULL CHECK (scope IN ('change', 'commit', 'diff')),
	anchor_revision_id TEXT NOT NULL REFERENCES revisions(id),
	commit_sha         TEXT NOT NULL DEFAULT '',
	path               TEXT NOT NULL DEFAULT '',
	blob_sha           TEXT NOT NULL DEFAULT '',
	side               TEXT NOT NULL DEFAULT '' CHECK (side IN ('', 'old', 'new')),
	start_line         INTEGER NOT NULL DEFAULT 0,
	end_line           INTEGER NOT NULL DEFAULT 0,
	lines              TEXT NOT NULL DEFAULT '[]',
	before_lines       TEXT NOT NULL DEFAULT '[]',
	after_lines        TEXT NOT NULL DEFAULT '[]',
	created_at         TEXT NOT NULL,
	updated_at         TEXT NOT NULL,
	UNIQUE (change_id, seq)
);

CREATE INDEX threads_commit ON threads (commit_sha) WHERE commit_sha != '';

CREATE TABLE comments (
	id           TEXT PRIMARY KEY,
	thread_id    TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
	-- NULL for a reply published outside a review.
	review_id    TEXT REFERENCES reviews(id) ON DELETE CASCADE,
	author_name  TEXT NOT NULL,
	author_email TEXT NOT NULL,
	body         TEXT NOT NULL,
	created_at   TEXT NOT NULL,
	updated_at   TEXT NOT NULL
);

CREATE INDEX comments_thread ON comments (thread_id);
`,
}
