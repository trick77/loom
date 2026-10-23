-- Full-text search over the user/assistant message corpus, powering the
-- conversation_search tool (cross-thread "did we discuss this before?").
--
-- A standalone (NOT external-content) FTS5 table that carries the identity
-- columns as UNINDEXED filter columns and indexes only `content`. role='tool'
-- rows are deliberately excluded — their large JSON / tool-output blobs add index
-- bloat and wreck result quality without any recall value.
--
-- The FTS rowid is pinned to messages.rowid (the messages table is not WITHOUT
-- ROWID, so it has one). That lets the delete/update triggers seek by rowid — the
-- FTS5 primary key — instead of filtering on the UNINDEXED message_id, which would
-- force a full index scan per row (costly when a thread delete cascades into many
-- message deletes). The read path still joins on the indexed messages.id via the
-- stored message_id column.
CREATE VIRTUAL TABLE message_fts USING fts5(
    message_id UNINDEXED,
    thread_id  UNINDEXED,
    user_id    UNINDEXED,
    content,
    tokenize = 'porter unicode61'
);

-- Backfill the existing history (user + assistant turns only), pinning each FTS
-- row's rowid to its messages.rowid.
INSERT INTO message_fts (rowid, message_id, thread_id, user_id, content)
SELECT rowid, id, thread_id, user_id, content
FROM messages
WHERE role IN ('user', 'assistant');

-- Keep the index in lockstep with the messages table via triggers, so it can
-- never drift regardless of which code path writes, edits, or deletes a message —
-- including the ON DELETE CASCADE row deletes when a thread or project is removed.
-- Deletes target rowid (a primary-key seek); a tool row's rowid was never indexed,
-- so its delete is a harmless no-op.
CREATE TRIGGER message_fts_ai AFTER INSERT ON messages
WHEN new.role IN ('user', 'assistant') BEGIN
    INSERT INTO message_fts (rowid, message_id, thread_id, user_id, content)
    VALUES (new.rowid, new.id, new.thread_id, new.user_id, new.content);
END;

CREATE TRIGGER message_fts_ad AFTER DELETE ON messages BEGIN
    DELETE FROM message_fts WHERE rowid = old.rowid;
END;

-- An edited message: drop the stale index row, then re-index only if the new row
-- is still a user/assistant turn (a tool row falls out of the index entirely).
CREATE TRIGGER message_fts_au AFTER UPDATE OF content ON messages BEGIN
    DELETE FROM message_fts WHERE rowid = old.rowid;
    INSERT INTO message_fts (rowid, message_id, thread_id, user_id, content)
    SELECT new.rowid, new.id, new.thread_id, new.user_id, new.content
    WHERE new.role IN ('user', 'assistant');
END;
