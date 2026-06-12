CREATE TABLE user_usage_totals (
    user_id           TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens     INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens  INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    web_searches      INTEGER NOT NULL DEFAULT 0,
    web_fetches       INTEGER NOT NULL DEFAULT 0,
    obscura_fetches   INTEGER NOT NULL DEFAULT 0,
    image_gens        INTEGER NOT NULL DEFAULT 0,
    chats_created     INTEGER NOT NULL DEFAULT 0,
    projects_created  INTEGER NOT NULL DEFAULT 0,
    updated_at        TEXT NOT NULL DEFAULT (datetime('now'))
);
