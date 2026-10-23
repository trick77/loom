-- Thread-scoped uploads: a document attached via the composer in a project-less
-- chat is private to that one thread, not user-global. thread_id is set only for
-- those thread-private documents; project documents and legacy global documents
-- leave it NULL.
--
-- Scope key (encoded into vec_chunks.project_id metadata at index time):
--   project_id set            -> '<projectID>'   (project scope, unchanged)
--   thread_id set             -> 'thread:<threadID>'
--   both NULL                 -> ''              (legacy user-global)

ALTER TABLE documents ADD COLUMN thread_id TEXT;

CREATE INDEX idx_documents_user_thread ON documents(user_id, thread_id);
