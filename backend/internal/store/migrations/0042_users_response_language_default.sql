-- migrate:no-transaction
-- Make '' the default response language. 0026 reset every 'auto' row to '' but
-- left DEFAULT 'auto', so a row inserted without the column still read as the
-- removed value. SQLite cannot alter a default in place, so the table is
-- rebuilt row for row. Foreign keys are off for the rebuild: every per-user
-- table cascades from users, and DROP TABLE with enforcement on would delete
-- their rows. Same shape as 0032.

PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE users_new (
    id TEXT PRIMARY KEY,
    oidc_subject TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    response_language TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_seen_at TEXT
);

INSERT INTO users_new (id, oidc_subject, username, email, display_name, role, response_language, created_at, updated_at, last_seen_at)
SELECT id, oidc_subject, username, email, display_name, role, response_language, created_at, updated_at, last_seen_at
FROM users;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

CREATE INDEX idx_users_role ON users(role);

COMMIT;
PRAGMA foreign_keys = ON;
