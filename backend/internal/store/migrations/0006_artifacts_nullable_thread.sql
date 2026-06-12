-- Make artifacts.thread_id nullable so user-uploaded documents can exist without
-- a chat thread (global uploads from the Artifacts browser). SQLite cannot drop a
-- NOT NULL constraint in place, so the table is rebuilt. documents.artifact_id
-- references artifacts(user_id, id); the rebuild preserves that key, and both
-- tables are empty of upload rows at migration time.

CREATE TABLE artifacts_new (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id TEXT,
    project_id TEXT,
    display_filename TEXT NOT NULL,
    volume_relpath TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    source TEXT NOT NULL DEFAULT 'assistant_generated',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, id),
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE CASCADE
);

INSERT INTO artifacts_new (id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at)
SELECT id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at
FROM artifacts;

DROP TABLE artifacts;
ALTER TABLE artifacts_new RENAME TO artifacts;

CREATE INDEX idx_artifacts_user_created ON artifacts(user_id, created_at DESC);
CREATE INDEX idx_artifacts_thread ON artifacts(user_id, thread_id, created_at DESC);
