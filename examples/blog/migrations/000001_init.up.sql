-- Posts, owned by an account.
--
-- account_id is indexed because the repository filters every query by it.
-- Without the index, every list is a sequential scan.
CREATE TABLE posts (
    id           TEXT PRIMARY KEY,
    account_id   TEXT NOT NULL,
    title        TEXT NOT NULL,
    slug         TEXT NOT NULL,
    body         TEXT NOT NULL DEFAULT '',
    published_at DATETIME,
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);

CREATE INDEX posts_account_id_idx ON posts (account_id);

-- The service also checks for a duplicate slug, for a good error message.
-- This index is what makes it true: the check alone is a race, because two
-- concurrent creates both see nothing and both insert.
CREATE UNIQUE INDEX posts_account_slug_idx ON posts (account_id, slug)
    WHERE deleted_at IS NULL;

-- The outbox. A message written here in the same transaction as the post
-- either lands with it or not at all.
CREATE TABLE outbox_messages (
    id           TEXT PRIMARY KEY,
    topic        TEXT NOT NULL,
    payload      BLOB NOT NULL,
    trace_id     TEXT,
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    available_at DATETIME NOT NULL,
    published_at DATETIME,
    created_at   DATETIME NOT NULL
);

CREATE INDEX outbox_pending_idx ON outbox_messages (available_at)
    WHERE published_at IS NULL;
