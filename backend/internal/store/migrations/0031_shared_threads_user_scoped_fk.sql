-- Bind a share to its thread through the owner: shared_threads referenced
-- threads(id) alone, so the schema accepted a share row whose user_id was not
-- the thread's owner and ownership rested on the handler. Every other child
-- table keys on (user_id, id); this rebuild makes shares do the same. SQLite
-- cannot alter a foreign key in place, so the table is rebuilt row for row.
-- Foreign keys are enforced on the connection; deferring them lets the copy
-- and the drop happen inside this one transaction.

PRAGMA defer_foreign_keys = ON;

CREATE TABLE shared_threads_new (
    id           TEXT PRIMARY KEY,
    share_id     TEXT NOT NULL UNIQUE,
    thread_id    TEXT NOT NULL UNIQUE,
    user_id      TEXT NOT NULL,
    shared       INTEGER NOT NULL DEFAULT 1 CHECK (shared IN (0, 1)),
    title        TEXT NOT NULL,
    snapshot     TEXT NOT NULL,
    artifact_ids TEXT NOT NULL DEFAULT '[]',
    snapshot_at  TEXT NOT NULL DEFAULT (datetime('now')),
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE
);

INSERT INTO shared_threads_new (id, share_id, thread_id, user_id, shared, title, snapshot, artifact_ids, snapshot_at, created_at, updated_at)
SELECT id, share_id, thread_id, user_id, shared, title, snapshot, artifact_ids, snapshot_at, created_at, updated_at
FROM shared_threads s
-- A share row whose user is not the thread's owner is exactly what the new key
-- forbids; copying one would fail the whole migration at COMMIT. Such a row was
-- never reachable through the handlers, so it is dropped.
WHERE EXISTS (SELECT 1 FROM threads t WHERE t.id = s.thread_id AND t.user_id = s.user_id);

DROP TABLE shared_threads;
ALTER TABLE shared_threads_new RENAME TO shared_threads;

CREATE INDEX idx_shared_threads_user ON shared_threads(user_id, created_at DESC);
