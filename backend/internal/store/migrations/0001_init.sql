CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    oidc_subject TEXT NOT NULL UNIQUE,
    username TEXT NOT NULL,
    email TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    response_language TEXT NOT NULL DEFAULT 'auto',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_seen_at TEXT
);

CREATE INDEX idx_users_role ON users(role);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

CREATE TABLE projects (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, id)
);

CREATE INDEX idx_projects_user_active ON projects(user_id, archived_at, updated_at DESC);

CREATE TABLE threads (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id TEXT,
    title TEXT NOT NULL,
    starred INTEGER NOT NULL DEFAULT 0 CHECK (starred IN (0, 1)),
    archived_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_message_at TEXT,
    UNIQUE(user_id, id),
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_threads_user_recent ON threads(user_id, archived_at, last_message_at DESC, updated_at DESC);
CREATE INDEX idx_threads_user_starred ON threads(user_id, starred, archived_at, updated_at DESC);
CREATE INDEX idx_threads_project ON threads(project_id, archived_at, updated_at DESC);

CREATE TABLE messages (
    id TEXT PRIMARY KEY,
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
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_messages_thread_created ON messages(thread_id, created_at, id);
CREATE INDEX idx_messages_user_created ON messages(user_id, created_at DESC);

CREATE TABLE artifacts (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id TEXT NOT NULL,
    project_id TEXT,
    display_filename TEXT NOT NULL,
    volume_relpath TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    source TEXT NOT NULL DEFAULT 'assistant_generated',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(user_id, id),
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE,
    FOREIGN KEY (user_id, project_id) REFERENCES projects(user_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_artifacts_user_created ON artifacts(user_id, created_at DESC);
CREATE INDEX idx_artifacts_thread ON artifacts(user_id, thread_id, created_at DESC);
