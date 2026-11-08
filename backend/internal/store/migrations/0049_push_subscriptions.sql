-- A browser's Web Push subscription. The endpoint is one browser profile's
-- push channel, so it is unique: a browser that signs in as someone else moves
-- its subscription to that user.

CREATE TABLE push_subscriptions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint TEXT NOT NULL UNIQUE,
    p256dh TEXT NOT NULL,
    auth TEXT NOT NULL,
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    -- Each visit re-saves the browser's subscription: the newest are the
    -- devices in use, the ones a user's cap keeps.
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_success_at TEXT
);

CREATE INDEX idx_push_subscriptions_user ON push_subscriptions(user_id, updated_at DESC);
