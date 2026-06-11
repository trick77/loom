CREATE TABLE user_memory (
    user_id TEXT NOT NULL PRIMARY KEY,
    content TEXT NOT NULL DEFAULT '',
    source_message_count INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
