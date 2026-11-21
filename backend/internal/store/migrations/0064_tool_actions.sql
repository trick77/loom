-- Tool actions: a call to an acting tool of a confirm server (mcp.json
-- "confirm": true) that the turn did not make but showed as a card. Only the
-- user's click runs it, with the stored arguments: outside content in the
-- turn can propose a call, never make one. result is the card's outcome as
-- JSON (text, statuses, a link), set once the call ran.
CREATE TABLE tool_actions (
    id         TEXT NOT NULL PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id  TEXT NOT NULL,
    tool       TEXT NOT NULL,
    arguments  TEXT NOT NULL,
    state      TEXT NOT NULL CHECK (state IN ('proposed', 'running', 'done', 'failed', 'dismissed')),
    result     TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    decided_at TEXT,
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_tool_actions_thread ON tool_actions(user_id, thread_id);
