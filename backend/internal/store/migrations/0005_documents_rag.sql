-- Document RAG: uploaded documents, their text chunks, and chunk embeddings.
--
-- Scope model (mirrors the volume layout): project_id set => project-scoped,
-- project_id NULL => user-global. Retrieval filters by user_id and scope.
--
-- Status lifecycle: 'pending' -> 'extracting' -> 'embedding' -> 'embedded';
-- plus 'stale' (file missing on disk) and 'error' (extraction/embedding failed).

CREATE TABLE documents (
    id            TEXT PRIMARY KEY,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id    TEXT,
    artifact_id   TEXT,
    volume_relpath TEXT NOT NULL,
    filename      TEXT NOT NULL,
    mime          TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL CHECK (size_bytes >= 0),
    status        TEXT NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending','extracting','embedding','embedded','stale','error')),
    error         TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    embedded_at   TEXT,
    UNIQUE(user_id, id),
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, artifact_id) REFERENCES artifacts(user_id, id) ON DELETE SET NULL
);

CREATE INDEX idx_documents_user_created ON documents(user_id, created_at DESC);
CREATE INDEX idx_documents_user_project ON documents(user_id, project_id);
CREATE INDEX idx_documents_status ON documents(status);

-- chunks.id is an INTEGER PRIMARY KEY (= SQLite rowid) so it can serve directly
-- as vec_chunks.rowid; this bridges the TEXT-id world to the INTEGER rowid that
-- the vec0 virtual table requires.
CREATE TABLE chunks (
    id          INTEGER PRIMARY KEY,
    document_id TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    project_id  TEXT,
    ordinal     INTEGER NOT NULL,
    text        TEXT NOT NULL,
    token_count INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (user_id, document_id) REFERENCES documents(user_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_chunks_document ON chunks(document_id, ordinal);
CREATE INDEX idx_chunks_user ON chunks(user_id);

-- Embedding store. user_id is a partition key (fast scoped KNN); project_id is a
-- metadata column ('' encodes the user-global scope, since vec0 metadata is not
-- nullable). Keyed 1:1 to chunks.id via rowid.
--
-- Note: neither FK CASCADE nor an AFTER DELETE trigger can reach a virtual table
-- (SQLite rejects vec0 in triggers as "unsafe use of virtual table"). The store
-- layer is therefore responsible for deleting the matching vec_chunks rows in the
-- same transaction whenever chunks are removed (document delete, re-index).
CREATE VIRTUAL TABLE vec_chunks USING vec0(
    embedding  float[1536],
    user_id    TEXT partition key,
    project_id TEXT
);
