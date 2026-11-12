-- migrate:no-transaction
-- A follow-up before an idea's due date is a notification of its own kind
-- (idea_due) that points at its idea. SQLite cannot change a CHECK or add a
-- foreign key in place, so notifications is rebuilt row for row (rowid kept)
-- with every index. Foreign keys are off for the rebuild, as in 0032 and 0054;
-- the file runs outside kit's transaction for the pragma to take.

PRAGMA foreign_keys = OFF;
BEGIN;

CREATE TABLE notifications_new (
    id          TEXT NOT NULL PRIMARY KEY,
    user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('schedule_run', 'schedule_disabled', 'idea_due')),
    schedule_id TEXT REFERENCES schedules(id) ON DELETE SET NULL,
    thread_id   TEXT REFERENCES threads(id) ON DELETE SET NULL,
    message_id  TEXT REFERENCES messages(id) ON DELETE SET NULL,
    idea_id     TEXT REFERENCES ideas(id) ON DELETE SET NULL,
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
CREATE INDEX idx_notifications_idea ON notifications(idea_id) WHERE idea_id IS NOT NULL;

COMMIT;
PRAGMA foreign_keys = ON;
