CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id TEXT NOT NULL,
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

CREATE INDEX idx_artifacts_user_created ON artifacts(user_id, created_at DESC);
CREATE INDEX idx_artifacts_thread ON artifacts(user_id, thread_id, created_at DESC);

ALTER TABLE messages ADD COLUMN artifacts TEXT NOT NULL DEFAULT '[]';
