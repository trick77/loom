-- Public, read-only share snapshots of a thread. A row exists only once a thread
-- has been shared at least once; shared=0 disables the public link without losing
-- the snapshot or rotating the share_id. The snapshot is a frozen, sanitized blob
-- (built by whitelist server-side) so later edits, compaction, or new messages
-- never leak into an already-shared link. artifact_ids is the allowlist of
-- generated-artifact ids the public artifact endpoints are permitted to serve.
-- Deleting a thread (or its project) cascades the share away, so the link 404s.
CREATE TABLE shared_threads (
    id           TEXT PRIMARY KEY,            -- internal id (chat.newID)
    share_id     TEXT NOT NULL UNIQUE,        -- public opaque token (chat.NewShareID)
    thread_id    TEXT NOT NULL UNIQUE,        -- one share row per thread
    user_id      TEXT NOT NULL,               -- owner / sharer
    shared       INTEGER NOT NULL DEFAULT 1 CHECK (shared IN (0, 1)),
    title        TEXT NOT NULL,
    snapshot     TEXT NOT NULL,
    artifact_ids TEXT NOT NULL DEFAULT '[]',
    snapshot_at  TEXT NOT NULL DEFAULT (datetime('now')),
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
);

CREATE INDEX idx_shared_threads_user ON shared_threads(user_id, created_at DESC);
