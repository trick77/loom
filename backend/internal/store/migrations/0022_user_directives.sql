-- Other instructions: explicit, user-steered standing instructions, stored
-- row-per-entry (unlike the derived user_memory blob). Mutated only through the
-- chat directive tools; shown read-only in the UI.
CREATE TABLE user_directives (
    id         TEXT NOT NULL PRIMARY KEY,
    user_id    TEXT NOT NULL,
    content    TEXT NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX idx_user_directives_user ON user_directives(user_id, position, created_at);
