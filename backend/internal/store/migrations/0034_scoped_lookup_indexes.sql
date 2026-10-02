-- A thread's transcript is read on every send and every thread view with
-- WHERE user_id = ? AND thread_id = ? ORDER BY rowid. Neither existing index
-- serves it: the planner took idx_messages_user_created, walked every message
-- the user owns and sorted the thread's rows in a temporary b-tree. rowid is
-- the implicit last key here, so this index covers the filter and the order.
CREATE INDEX idx_messages_user_thread ON messages(user_id, thread_id);

-- Chunk lookups are always (user, document). With one single-column match on
-- each of the two old indexes the planner picked idx_chunks_user, i.e. every
-- chunk the user owns per document. idx_chunks_user is a prefix of this one.
CREATE INDEX idx_chunks_user_document ON chunks(user_id, document_id, ordinal);
DROP INDEX IF EXISTS idx_chunks_user;

-- documents.artifact_id ON DELETE SET NULL looks rows up by artifact_id alone,
-- which idx_documents_user_artifact (user_id first) cannot seek: every artifact
-- delete, and so every thread or project delete, scanned the table.
CREATE INDEX idx_documents_artifact ON documents(artifact_id);

-- Superseded by idx_threads_user_recency (0030): every thread list orders by
-- COALESCE(last_message_at, updated_at), which these cannot serve, yet each
-- message insert still maintained both.
DROP INDEX IF EXISTS idx_threads_user_recent;
DROP INDEX IF EXISTS idx_threads_user_starred;
