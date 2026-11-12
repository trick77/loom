-- An assistant message from a turn that read outside content (a web page,
-- another tool's output), or from any later turn of the same thread: the
-- fact extraction skips it, so a page's text never becomes a "fact about the
-- user". User messages are never flagged. Existing rows stay 0.
ALTER TABLE messages ADD COLUMN read_outside INTEGER NOT NULL DEFAULT 0 CHECK (read_outside IN (0, 1));

-- Every assistant insert asks whether its thread holds a flagged answer.
CREATE INDEX idx_messages_read_outside ON messages(user_id, thread_id) WHERE read_outside = 1;
