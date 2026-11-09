-- migrate:no-transaction
-- Make every TEXT primary key NOT NULL. SQLite lets a non-INTEGER PRIMARY KEY
-- column of a rowid table hold NULL (a legacy quirk), and NULLs never collide:
-- a NULL user_id never matches ON CONFLICT(user_id) in user_usage_totals or
-- user_fact_cursor, so each upsert would add one more row. SQLite cannot add a
-- constraint in place, so every such table is rebuilt row for row.
--
-- Go always sets the key, so no NULL-key row is expected. One that exists
-- anyway is deleted first: no foreign key can point at it, and a read that
-- selects it fails to scan the NULL into Go's string id. The delete runs while
-- the table and its triggers still exist, so a message's search row goes too.
--
-- rowid is copied explicitly: message_fts is keyed by messages.rowid, and
-- messages, queued_messages, schedule_runs, push_subscriptions and others
-- order by rowid. Columns stay in their current order (ALTER TABLE ADD COLUMN
-- appended some), and every default, CHECK, UNIQUE, foreign key, index and
-- trigger is kept as it is. Foreign keys are off for the rebuild, as in 0032
-- and 0042: DROP TABLE with enforcement on would cascade into every child.
--
-- messages' search triggers go with its DROP TABLE and come back in 0055:
-- kit runs a no-transaction file statement by statement, split on each
-- semicolon outside quotes and comments, which cuts a trigger's BEGIN … END
-- body apart.

PRAGMA foreign_keys = OFF;
BEGIN;

DELETE FROM users WHERE id IS NULL;
DELETE FROM projects WHERE id IS NULL;
DELETE FROM schedules WHERE id IS NULL;
DELETE FROM threads WHERE id IS NULL;
DELETE FROM messages WHERE id IS NULL;
DELETE FROM queued_messages WHERE id IS NULL;
DELETE FROM shared_threads WHERE id IS NULL;
DELETE FROM artifacts WHERE id IS NULL;
DELETE FROM documents WHERE id IS NULL;
DELETE FROM schedule_runs WHERE id IS NULL;
DELETE FROM notifications WHERE id IS NULL;
DELETE FROM push_subscriptions WHERE id IS NULL;
DELETE FROM settings WHERE key IS NULL;
DELETE FROM user_facts WHERE id IS NULL;
DELETE FROM user_fact_cursor WHERE user_id IS NULL;
DELETE FROM user_location_state WHERE user_id IS NULL;
DELETE FROM user_usage_totals WHERE user_id IS NULL;

-- users

CREATE TABLE users_new (
    id TEXT NOT NULL PRIMARY KEY,
    oidc_subject TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    response_language TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_seen_at TEXT,
    timezone TEXT NOT NULL DEFAULT ''
);

INSERT INTO users_new (rowid, id, oidc_subject, username, email, display_name, role, response_language, created_at, updated_at, last_seen_at, timezone)
SELECT rowid, id, oidc_subject, username, email, display_name, role, response_language, created_at, updated_at, last_seen_at, timezone
FROM users;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

CREATE INDEX idx_users_role ON users(role);

-- projects

CREATE TABLE projects_new (
    id TEXT NOT NULL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    starred INTEGER NOT NULL DEFAULT 0 CHECK (starred IN (0, 1)),
    auto_description_generated_at TEXT,
    last_activity_at TEXT,
    description_user_edited INTEGER NOT NULL DEFAULT 0,
    description_source_thread_count INTEGER NOT NULL DEFAULT 0,
    UNIQUE(user_id, id)
);

INSERT INTO projects_new (rowid, id, user_id, name, description, archived_at, created_at, updated_at, starred, auto_description_generated_at, last_activity_at, description_user_edited, description_source_thread_count)
SELECT rowid, id, user_id, name, description, archived_at, created_at, updated_at, starred, auto_description_generated_at, last_activity_at, description_user_edited, description_source_thread_count
FROM projects;

DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;

CREATE INDEX idx_projects_user_activity ON projects(user_id, archived_at, last_activity_at DESC);

-- schedules

CREATE TABLE schedules_new (
    id              TEXT NOT NULL PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    prompt          TEXT NOT NULL,
    -- interval_text is what the user typed; cron_template is the gate model's
    -- reading of it with free fields as "~"; cron is the template with every
    -- "~" drawn, in timezone.
    interval_text   TEXT NOT NULL,
    cron_template   TEXT NOT NULL,
    cron            TEXT NOT NULL,
    timezone        TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    next_run_at     TEXT,
    -- running_at is set while a run holds the schedule (the worker's claim).
    running_at      TEXT,
    -- disabled_reason is set only when loom disabled the schedule itself.
    disabled_reason TEXT,
    disabled_at     TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO schedules_new (rowid, id, user_id, name, prompt, interval_text, cron_template, cron, timezone, enabled, next_run_at, running_at, disabled_reason, disabled_at, created_at, updated_at)
SELECT rowid, id, user_id, name, prompt, interval_text, cron_template, cron, timezone, enabled, next_run_at, running_at, disabled_reason, disabled_at, created_at, updated_at
FROM schedules;

DROP TABLE schedules;
ALTER TABLE schedules_new RENAME TO schedules;

CREATE INDEX idx_schedules_due ON schedules(next_run_at) WHERE enabled = 1;
CREATE INDEX idx_schedules_user ON schedules(user_id, created_at);

-- threads

CREATE TABLE threads_new (
    id TEXT NOT NULL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id TEXT,
    title TEXT NOT NULL,
    starred INTEGER NOT NULL DEFAULT 0 CHECK (starred IN (0, 1)),
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_message_at TEXT,
    category TEXT NOT NULL DEFAULT '',
    image_model TEXT NOT NULL DEFAULT '',
    schedule_id TEXT REFERENCES schedules(id) ON DELETE SET NULL,
    UNIQUE(user_id, id),
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE CASCADE
);

INSERT INTO threads_new (rowid, id, user_id, project_id, title, starred, archived_at, created_at, updated_at, last_message_at, category, image_model, schedule_id)
SELECT rowid, id, user_id, project_id, title, starred, archived_at, created_at, updated_at, last_message_at, category, image_model, schedule_id
FROM threads;

DROP TABLE threads;
ALTER TABLE threads_new RENAME TO threads;

CREATE INDEX idx_threads_project ON threads(project_id, archived_at, updated_at DESC);
CREATE INDEX idx_threads_schedule ON threads(schedule_id) WHERE schedule_id IS NOT NULL;
CREATE INDEX idx_threads_user_recency
    ON threads(user_id, archived_at, COALESCE(last_message_at, updated_at) DESC, updated_at DESC, id DESC);

-- messages

CREATE TABLE messages_new (
    id TEXT NOT NULL PRIMARY KEY,
    thread_id TEXT NOT NULL,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'tool')),
    content TEXT NOT NULL,
    reasoning_content TEXT NOT NULL DEFAULT '',
    tool_calls TEXT NOT NULL DEFAULT '[]',
    citations TEXT NOT NULL DEFAULT '[]',
    artifacts TEXT NOT NULL DEFAULT '[]',
    activity_trace TEXT NOT NULL DEFAULT '[]',
    prompt_tokens INTEGER,
    completion_tokens INTEGER,
    total_tokens INTEGER,
    cached_tokens INTEGER,
    reasoning_tokens INTEGER,
    duration_ms INTEGER,
    model TEXT,
    reasoning_effort TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    attachments TEXT NOT NULL DEFAULT '[]',
    content_blocks TEXT NOT NULL DEFAULT '[]',
    context_tokens INTEGER,
    pasted_texts TEXT NOT NULL DEFAULT '[]',
    cost_nano_usd INTEGER,
    client_message_id TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE
);

INSERT INTO messages_new (rowid, id, thread_id, user_id, role, content, reasoning_content, tool_calls, citations, artifacts, activity_trace, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, duration_ms, model, reasoning_effort, created_at, attachments, content_blocks, context_tokens, pasted_texts, cost_nano_usd, client_message_id)
SELECT rowid, id, thread_id, user_id, role, content, reasoning_content, tool_calls, citations, artifacts, activity_trace, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, duration_ms, model, reasoning_effort, created_at, attachments, content_blocks, context_tokens, pasted_texts, cost_nano_usd, client_message_id
FROM messages;

DROP TABLE messages;
ALTER TABLE messages_new RENAME TO messages;

CREATE INDEX idx_messages_thread_created ON messages(thread_id, created_at, id);
CREATE INDEX idx_messages_user_created ON messages(user_id, created_at DESC);
CREATE INDEX idx_messages_user_thread ON messages(user_id, thread_id);

-- queued_messages

CREATE TABLE queued_messages_new (
    id                TEXT NOT NULL PRIMARY KEY,
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

INSERT INTO queued_messages_new (rowid, id, thread_id, user_id, content, pasted_texts, client_message_id, created_at)
SELECT rowid, id, thread_id, user_id, content, pasted_texts, client_message_id, created_at
FROM queued_messages;

DROP TABLE queued_messages;
ALTER TABLE queued_messages_new RENAME TO queued_messages;

CREATE INDEX idx_queued_messages_thread ON queued_messages(user_id, thread_id);

-- shared_threads

CREATE TABLE shared_threads_new (
    id           TEXT NOT NULL PRIMARY KEY,
    share_id     TEXT NOT NULL UNIQUE,
    thread_id    TEXT NOT NULL UNIQUE,
    user_id      TEXT NOT NULL,
    shared       INTEGER NOT NULL DEFAULT 1 CHECK (shared IN (0, 1)),
    title        TEXT NOT NULL,
    snapshot     TEXT NOT NULL,
    artifact_ids TEXT NOT NULL DEFAULT '[]',
    snapshot_at  TEXT NOT NULL DEFAULT (datetime('now')),
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE
);

INSERT INTO shared_threads_new (rowid, id, share_id, thread_id, user_id, shared, title, snapshot, artifact_ids, snapshot_at, created_at, updated_at)
SELECT rowid, id, share_id, thread_id, user_id, shared, title, snapshot, artifact_ids, snapshot_at, created_at, updated_at
FROM shared_threads;

DROP TABLE shared_threads;
ALTER TABLE shared_threads_new RENAME TO shared_threads;

CREATE INDEX idx_shared_threads_user ON shared_threads(user_id, created_at DESC);

-- artifacts

CREATE TABLE artifacts_new (
    id TEXT NOT NULL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id TEXT,
    project_id TEXT,
    display_filename TEXT NOT NULL,
    volume_relpath TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    source TEXT NOT NULL DEFAULT 'assistant_generated',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    deleted_at TEXT,
    thumbnail_relpath TEXT,
    UNIQUE(user_id, id),
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE CASCADE
);

INSERT INTO artifacts_new (rowid, id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, deleted_at, thumbnail_relpath)
SELECT rowid, id, user_id, thread_id, project_id, display_filename, volume_relpath, mime_type, size_bytes, source, created_at, deleted_at, thumbnail_relpath
FROM artifacts;

DROP TABLE artifacts;
ALTER TABLE artifacts_new RENAME TO artifacts;

CREATE INDEX idx_artifacts_thread ON artifacts(user_id, thread_id, created_at DESC);
CREATE INDEX idx_artifacts_user_created ON artifacts(user_id, created_at DESC);
CREATE INDEX idx_artifacts_user_project ON artifacts(user_id, project_id, created_at);

-- documents

CREATE TABLE documents_new (
    id             TEXT NOT NULL PRIMARY KEY,
    user_id        TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id     TEXT,
    artifact_id    TEXT REFERENCES artifacts(id) ON DELETE SET NULL,
    volume_relpath TEXT NOT NULL,
    filename       TEXT NOT NULL,
    mime           TEXT NOT NULL,
    size_bytes     INTEGER NOT NULL CHECK (size_bytes >= 0),
    status         TEXT NOT NULL DEFAULT 'pending'
                        CHECK (status IN ('pending','extracting','embedding','embedded','stale','error')),
    error          TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL DEFAULT (datetime('now')),
    embedded_at    TEXT,
    thread_id      TEXT,
    full_text      TEXT NOT NULL DEFAULT '',
    UNIQUE(user_id, id),
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE CASCADE
);

INSERT INTO documents_new (rowid, id, user_id, project_id, artifact_id, volume_relpath, filename, mime, size_bytes, status, error, created_at, embedded_at, thread_id, full_text)
SELECT rowid, id, user_id, project_id, artifact_id, volume_relpath, filename, mime, size_bytes, status, error, created_at, embedded_at, thread_id, full_text
FROM documents;

DROP TABLE documents;
ALTER TABLE documents_new RENAME TO documents;

CREATE INDEX idx_documents_artifact ON documents(artifact_id);
CREATE INDEX idx_documents_status ON documents(status);
CREATE INDEX idx_documents_user_artifact ON documents(user_id, artifact_id);
CREATE INDEX idx_documents_user_created ON documents(user_id, created_at DESC);
CREATE INDEX idx_documents_user_project ON documents(user_id, project_id);
CREATE INDEX idx_documents_user_thread ON documents(user_id, thread_id);

-- schedule_runs

CREATE TABLE schedule_runs_new (
    id            TEXT NOT NULL PRIMARY KEY,
    schedule_id   TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id    TEXT REFERENCES messages(id) ON DELETE SET NULL,
    started_at    TEXT NOT NULL DEFAULT (datetime('now')),
    finished_at   TEXT,
    status        TEXT NOT NULL DEFAULT 'running'
                  CHECK (status IN ('running', 'new', 'nothing_new', 'token_limit', 'timeout', 'failed')),
    rounds        INTEGER NOT NULL DEFAULT 0,
    tokens        INTEGER NOT NULL DEFAULT 0,
    cost_nano_usd INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    error         TEXT NOT NULL DEFAULT '',
    thread_id     TEXT REFERENCES threads(id) ON DELETE SET NULL
);

INSERT INTO schedule_runs_new (rowid, id, schedule_id, user_id, message_id, started_at, finished_at, status, rounds, tokens, cost_nano_usd, duration_ms, error, thread_id)
SELECT rowid, id, schedule_id, user_id, message_id, started_at, finished_at, status, rounds, tokens, cost_nano_usd, duration_ms, error, thread_id
FROM schedule_runs;

DROP TABLE schedule_runs;
ALTER TABLE schedule_runs_new RENAME TO schedule_runs;

CREATE INDEX idx_schedule_runs_message ON schedule_runs(message_id) WHERE message_id IS NOT NULL;
CREATE INDEX idx_schedule_runs_schedule ON schedule_runs(schedule_id, started_at DESC);
CREATE INDEX idx_schedule_runs_thread ON schedule_runs(thread_id) WHERE thread_id IS NOT NULL;
CREATE INDEX idx_schedule_runs_user ON schedule_runs(user_id, schedule_id, started_at DESC);

-- notifications

CREATE TABLE notifications_new (
    id          TEXT NOT NULL PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('schedule_run', 'schedule_disabled')),
    schedule_id TEXT REFERENCES schedules(id) ON DELETE SET NULL,
    thread_id   TEXT REFERENCES threads(id) ON DELETE SET NULL,
    message_id  TEXT REFERENCES messages(id) ON DELETE SET NULL,
    title       TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    read_at     TEXT
);

INSERT INTO notifications_new (rowid, id, user_id, kind, schedule_id, thread_id, message_id, title, body, created_at, read_at)
SELECT rowid, id, user_id, kind, schedule_id, thread_id, message_id, title, body, created_at, read_at
FROM notifications;

DROP TABLE notifications;
ALTER TABLE notifications_new RENAME TO notifications;

CREATE INDEX idx_notifications_message ON notifications(message_id) WHERE message_id IS NOT NULL;
CREATE INDEX idx_notifications_schedule ON notifications(schedule_id) WHERE schedule_id IS NOT NULL;
CREATE INDEX idx_notifications_thread ON notifications(thread_id) WHERE thread_id IS NOT NULL;
CREATE INDEX idx_notifications_user ON notifications(user_id, created_at DESC);

-- push_subscriptions

CREATE TABLE push_subscriptions_new (
    id TEXT NOT NULL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    -- The VAPID public key the browser subscribed with. A rotated session
    -- secret derives a new one; rows with the old key can never be delivered.
    vapid_key TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    -- Each visit re-saves the browser's subscription: the newest are the
    -- devices in use, the ones a user's cap keeps.
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_success_at TEXT
);

INSERT INTO push_subscriptions_new (rowid, id, user_id, endpoint, p256dh, auth, vapid_key, user_agent, created_at, updated_at, last_success_at)
SELECT rowid, id, user_id, endpoint, p256dh, auth, vapid_key, user_agent, created_at, updated_at, last_success_at
FROM push_subscriptions;

DROP TABLE push_subscriptions;
ALTER TABLE push_subscriptions_new RENAME TO push_subscriptions;

CREATE INDEX idx_push_subscriptions_user ON push_subscriptions(user_id, updated_at DESC);

-- settings

CREATE TABLE settings_new (
    key   TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO settings_new (rowid, key, value)
SELECT rowid, key, value
FROM settings;

DROP TABLE settings;
ALTER TABLE settings_new RENAME TO settings;

-- user_facts

CREATE TABLE user_facts_new (
    id TEXT NOT NULL PRIMARY KEY,
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

INSERT INTO user_facts_new (rowid, id, user_id, category, content, durable, origin, first_seen, last_seen, retired_at, retire_reason, superseded_by, created_at, updated_at)
SELECT rowid, id, user_id, category, content, durable, origin, first_seen, last_seen, retired_at, retire_reason, superseded_by, created_at, updated_at
FROM user_facts;

DROP TABLE user_facts;
ALTER TABLE user_facts_new RENAME TO user_facts;

CREATE INDEX idx_user_facts_active ON user_facts(user_id, retired_at);
CREATE INDEX idx_user_facts_seen ON user_facts(user_id, last_seen);

-- user_fact_cursor

CREATE TABLE user_fact_cursor_new (
    user_id TEXT NOT NULL PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    last_created_at TEXT NOT NULL DEFAULT '',
    last_message_id TEXT NOT NULL DEFAULT '',
    last_refresh_at TEXT,
    last_consolidated_at TEXT
);

INSERT INTO user_fact_cursor_new (rowid, user_id, last_created_at, last_message_id, last_refresh_at, last_consolidated_at)
SELECT rowid, user_id, last_created_at, last_message_id, last_refresh_at, last_consolidated_at
FROM user_fact_cursor;

DROP TABLE user_fact_cursor;
ALTER TABLE user_fact_cursor_new RENAME TO user_fact_cursor;

-- user_location_state

CREATE TABLE user_location_state_new (
    user_id TEXT NOT NULL PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    facts_hash TEXT NOT NULL,
    resolved_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO user_location_state_new (rowid, user_id, facts_hash, resolved_at)
SELECT rowid, user_id, facts_hash, resolved_at
FROM user_location_state;

DROP TABLE user_location_state;
ALTER TABLE user_location_state_new RENAME TO user_location_state;

-- user_usage_totals

CREATE TABLE user_usage_totals_new (
    user_id            TEXT NOT NULL PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prompt_tokens      INTEGER NOT NULL DEFAULT 0,
    completion_tokens  INTEGER NOT NULL DEFAULT 0,
    cached_tokens      INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens   INTEGER NOT NULL DEFAULT 0,
    total_tokens       INTEGER NOT NULL DEFAULT 0,
    web_searches       INTEGER NOT NULL DEFAULT 0,
    web_fetches        INTEGER NOT NULL DEFAULT 0,
    obscura_fetches    INTEGER NOT NULL DEFAULT 0,
    image_gens         INTEGER NOT NULL DEFAULT 0,
    chats_created      INTEGER NOT NULL DEFAULT 0,
    projects_created   INTEGER NOT NULL DEFAULT 0,
    updated_at         TEXT NOT NULL DEFAULT (datetime('now')),
    embedding_tokens   INTEGER NOT NULL DEFAULT 0,
    embedding_requests INTEGER NOT NULL DEFAULT 0,
    cost_nano_usd      INTEGER NOT NULL DEFAULT 0,
    code_runs          INTEGER NOT NULL DEFAULT 0
);

INSERT INTO user_usage_totals_new (rowid, user_id, prompt_tokens, completion_tokens, cached_tokens, reasoning_tokens, total_tokens, web_searches, web_fetches, obscura_fetches, image_gens, chats_created, projects_created, updated_at, embedding_tokens, embedding_requests, cost_nano_usd, code_runs)
SELECT rowid, user_id, prompt_tokens, completion_tokens, cached_tokens, reasoning_tokens, total_tokens, web_searches, web_fetches, obscura_fetches, image_gens, chats_created, projects_created, updated_at, embedding_tokens, embedding_requests, cost_nano_usd, code_runs
FROM user_usage_totals;

DROP TABLE user_usage_totals;
ALTER TABLE user_usage_totals_new RENAME TO user_usage_totals;

COMMIT;
PRAGMA foreign_keys = ON;
