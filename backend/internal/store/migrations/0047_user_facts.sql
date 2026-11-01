-- User memory as one row per fact. A refresh adds, reinforces and retires facts
-- instead of rewriting one text blob, so a fact the model does not mention is
-- never at risk. first_seen/last_seen are message dates; the time buckets
-- (top of mind, recent, earlier, long-term) are computed, never stored.
-- user_memory stays as the read-only source of the one-time conversion.

CREATE TABLE user_facts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category TEXT NOT NULL CHECK (category IN ('work', 'personal', 'interest')),
    content TEXT NOT NULL,
    durable INTEGER NOT NULL DEFAULT 0 CHECK (durable IN (0, 1)),
    origin TEXT NOT NULL CHECK (origin IN ('extracted', 'tool', 'converted')),
    first_seen TEXT NOT NULL,
    last_seen TEXT NOT NULL,
    retired_at TEXT,
    retire_reason TEXT CHECK (retire_reason IN ('superseded', 'user', 'thread_deleted')),
    superseded_by TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_user_facts_active ON user_facts(user_id, retired_at);
CREATE INDEX idx_user_facts_seen ON user_facts(user_id, last_seen);

-- The threads a fact was learned or reinforced in. Deleting a thread drops its
-- rows; an extracted fact left without any source is retired.
CREATE TABLE user_fact_sources (
    fact_id TEXT NOT NULL REFERENCES user_facts(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL,
    thread_id TEXT NOT NULL,
    PRIMARY KEY (fact_id, thread_id),
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_user_fact_sources_thread ON user_fact_sources(user_id, thread_id);

-- Refresh cursor: the last message folded in, by (created_at, id). Not rowid:
-- messages has a TEXT primary key, so its rowid is implicit and VACUUM may
-- renumber it.
CREATE TABLE user_fact_cursor (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    last_created_at TEXT NOT NULL DEFAULT '',
    last_message_id TEXT NOT NULL DEFAULT '',
    last_refresh_at TEXT,
    last_consolidated_at TEXT
);
