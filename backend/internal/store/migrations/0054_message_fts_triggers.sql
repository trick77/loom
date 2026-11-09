-- Restore messages' search triggers, dropped with the table by 0053's
-- rebuild. kit splits a no-transaction file like 0053 on each semicolon
-- outside quotes and comments, which cuts a BEGIN … END body apart, so they
-- live in this transactional file. Same text as 0024.

CREATE TRIGGER message_fts_ai AFTER INSERT ON messages
WHEN new.role IN ('user', 'assistant') BEGIN
    INSERT INTO message_fts (rowid, message_id, thread_id, user_id, content)
    VALUES (new.rowid, new.id, new.thread_id, new.user_id, new.content);
END;

CREATE TRIGGER message_fts_ad AFTER DELETE ON messages BEGIN
    DELETE FROM message_fts WHERE rowid = old.rowid;
END;

CREATE TRIGGER message_fts_au AFTER UPDATE OF content ON messages BEGIN
    DELETE FROM message_fts WHERE rowid = old.rowid;
    INSERT INTO message_fts (rowid, message_id, thread_id, user_id, content)
    SELECT new.rowid, new.id, new.thread_id, new.user_id, new.content
    WHERE new.role IN ('user', 'assistant');
END;
