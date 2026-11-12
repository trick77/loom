-- Ideas: something the user wants to follow up later, always with the reason
-- why. A proposal from the idea tools (state 'proposed') is invisible
-- everywhere but its card in the thread until the user confirms it there.
-- thread_title is a snapshot: an idea survives its deleted thread unchanged,
-- had_thread tells "thread deleted" from "noted without a thread". due_on and
-- remind_on are civil dates ('YYYY-MM-DD') in the user's zone. deleted_at is
-- a soft delete, restorable for 30 days.
CREATE TABLE ideas (
    id                  TEXT NOT NULL PRIMARY KEY,
    user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    state               TEXT NOT NULL CHECK (state IN ('proposed', 'open', 'done')),
    title               TEXT NOT NULL,
    reason              TEXT NOT NULL,
    excerpt             TEXT NOT NULL DEFAULT '',
    thread_id           TEXT REFERENCES threads(id) ON DELETE SET NULL,
    message_id          TEXT REFERENCES messages(id) ON DELETE SET NULL,
    thread_title        TEXT NOT NULL DEFAULT '',
    had_thread          INTEGER NOT NULL DEFAULT 0 CHECK (had_thread IN (0, 1)),
    due_on              TEXT,
    remind_on           TEXT,
    reminded_at         TEXT,
    remind_dismissed_at TEXT,
    done_at             TEXT,
    deleted_at          TEXT,
    created_at          TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at          TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_ideas_user_state ON ideas(user_id, state, deleted_at, created_at DESC);
CREATE INDEX idx_ideas_remind ON ideas(remind_on) WHERE reminded_at IS NULL AND remind_on IS NOT NULL;
CREATE INDEX idx_ideas_thread ON ideas(thread_id) WHERE thread_id IS NOT NULL;
CREATE INDEX idx_ideas_message ON ideas(message_id) WHERE message_id IS NOT NULL;
