-- Messages the user sent while a turn was still running on the thread. A row
-- waits here until the running turn picks it up at a tool boundary or in a
-- follow-up turn; then it moves into messages in the same transaction, so it
-- is never lost and never stored twice. The messages row gets its rowid at
-- that moment, which keeps the transcript in the order things happened.
CREATE TABLE queued_messages (
    id                TEXT PRIMARY KEY,
    thread_id         TEXT NOT NULL,
    user_id           TEXT NOT NULL,
    content           TEXT NOT NULL,
    pasted_texts      TEXT NOT NULL DEFAULT '[]',
    client_message_id TEXT NOT NULL,
    created_at        TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE (thread_id, client_message_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
);

CREATE INDEX idx_queued_messages_thread ON queued_messages(user_id, thread_id);
