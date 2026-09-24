-- loom:foreign_keys=off
-- Key documents.artifact_id on artifacts(id) alone. The composite
-- (user_id, artifact_id) key carried ON DELETE SET NULL, which on an artifact
-- delete would null user_id as well and abort on its NOT NULL constraint; the
-- thread-delete path has been detaching artifacts first to avoid it. SQLite
-- cannot alter a foreign key in place, so the table is rebuilt row for row.
-- Foreign keys are off for this file (see the directive above): chunks
-- cascade from documents, and DROP TABLE with enforcement on would delete
-- every chunk. The user/project keys and every index are preserved, and a
-- (user_id, artifact_id) index is added so an artifact delete no longer scans
-- the table.

CREATE TABLE documents_new (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id     TEXT,
    artifact_id    TEXT REFERENCES artifacts(id) ON DELETE SET NULL,
    volume_relpath TEXT NOT NULL,
    filename       TEXT NOT NULL,
    mime           TEXT NOT NULL,
    size_bytes     INTEGER NOT NULL CHECK (size_bytes >= 0),
    status         TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','extracting','embedding','embedded','stale','error')),
    error          TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    embedded_at    TEXT,
    thread_id      TEXT,
    full_text      TEXT NOT NULL DEFAULT '',
    UNIQUE(user_id, id),
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE CASCADE
);

INSERT INTO documents_new (id, user_id, project_id, artifact_id, volume_relpath, filename, mime, size_bytes, status, error, created_at, embedded_at, thread_id, full_text)
SELECT id, user_id, project_id, artifact_id, volume_relpath, filename, mime, size_bytes, status, error, created_at, embedded_at, thread_id, full_text
FROM documents;

DROP TABLE documents;
ALTER TABLE documents_new RENAME TO documents;

CREATE INDEX idx_documents_user_created ON documents(user_id, created_at DESC);
CREATE INDEX idx_documents_user_project ON documents(user_id, project_id);
CREATE INDEX idx_documents_status ON documents(status);
CREATE INDEX idx_documents_user_thread ON documents(user_id, thread_id);
CREATE INDEX idx_documents_user_artifact ON documents(user_id, artifact_id);
