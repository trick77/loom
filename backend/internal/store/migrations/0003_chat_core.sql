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
    tool_calls TEXT NOT NULL DEFAULT '[]',
    citations TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (user_id, thread_id) REFERENCES threads(user_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_messages_thread_created ON messages(thread_id, created_at, id);
CREATE INDEX idx_messages_user_created ON messages(user_id, created_at DESC);
