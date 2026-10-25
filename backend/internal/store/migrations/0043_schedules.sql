-- Scheduled tasks: a prompt that runs on its own on a cron the user described
-- in words. Each run appends a turn to the schedule's own read-only thread and
-- notifies the user only about what earlier runs did not report.
CREATE TABLE schedules (
    id              TEXT PRIMARY KEY,
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
    thread_id       TEXT REFERENCES threads(id) ON DELETE SET NULL,
    next_run_at     TEXT,
    -- running_at is set while a run holds the schedule (the worker's claim).
    running_at      TEXT,
    -- disabled_reason is set only when loom disabled the schedule itself.
    disabled_reason TEXT,
    disabled_at     TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_schedules_user ON schedules(user_id, created_at);
CREATE INDEX idx_schedules_due ON schedules(next_run_at) WHERE enabled = 1;
CREATE UNIQUE INDEX idx_schedules_thread ON schedules(thread_id) WHERE thread_id IS NOT NULL;

CREATE TABLE schedule_runs (
    id            TEXT PRIMARY KEY,
    schedule_id   TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id    TEXT REFERENCES messages(id) ON DELETE SET NULL,
    started_at    TEXT NOT NULL DEFAULT (datetime('now')),
    finished_at   TEXT,
    status        TEXT NOT NULL DEFAULT 'running'
                  CHECK (status IN ('running', 'new', 'nothing_new', 'token_limit', 'timeout', 'failed')),
    new_count     INTEGER NOT NULL DEFAULT 0,
    rounds        INTEGER NOT NULL DEFAULT 0,
    tokens        INTEGER NOT NULL DEFAULT 0,
    cost_nano_usd INTEGER NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    error         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_schedule_runs_schedule ON schedule_runs(schedule_id, started_at DESC);
CREATE INDEX idx_schedule_runs_user ON schedule_runs(user_id, schedule_id, started_at DESC);

-- What a schedule already reported: never announced again.
CREATE TABLE schedule_findings (
    schedule_id   TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_key      TEXT NOT NULL,
    label         TEXT NOT NULL,
    source_url    TEXT NOT NULL DEFAULT '',
    run_id        TEXT REFERENCES schedule_runs(id) ON DELETE SET NULL,
    first_seen_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (schedule_id, item_key)
);

-- How runs got their information, recorded by loom from tool outcomes.
CREATE TABLE schedule_sources (
    schedule_id     TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    url_key         TEXT NOT NULL,
    prefer          TEXT NOT NULL DEFAULT 'fetch' CHECK (prefer IN ('fetch', 'browser')),
    fetch_failed_at TEXT,
    browser_ok_at   TEXT,
    runs            INTEGER NOT NULL DEFAULT 0,
    finds           INTEGER NOT NULL DEFAULT 0,
    last_used_at    TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (schedule_id, url_key)
);

-- Search queries the runs issued, with the finds their results led to.
CREATE TABLE schedule_queries (
    schedule_id  TEXT NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    query        TEXT NOT NULL,
    runs         INTEGER NOT NULL DEFAULT 0,
    finds        INTEGER NOT NULL DEFAULT 0,
    empty_runs   INTEGER NOT NULL DEFAULT 0,
    last_used_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (schedule_id, query)
);

CREATE TABLE notifications (
    id          TEXT PRIMARY KEY,
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

CREATE INDEX idx_notifications_user ON notifications(user_id, created_at DESC);

-- A thread continued from another thread's answer ("Continue in new thread").
ALTER TABLE threads ADD COLUMN branched_from_thread_id TEXT REFERENCES threads(id) ON DELETE SET NULL;
ALTER TABLE threads ADD COLUMN branched_from_message_id TEXT REFERENCES messages(id) ON DELETE SET NULL;

-- Every foreign key child column gets an index: deleting a parent row (a
-- thread with hundreds of messages) looks its children up once per row, and
-- without one each lookup scans the whole table under the write lock.
CREATE INDEX idx_threads_branched_thread ON threads(branched_from_thread_id) WHERE branched_from_thread_id IS NOT NULL;
CREATE INDEX idx_threads_branched_message ON threads(branched_from_message_id) WHERE branched_from_message_id IS NOT NULL;
CREATE INDEX idx_notifications_schedule ON notifications(schedule_id) WHERE schedule_id IS NOT NULL;
CREATE INDEX idx_notifications_thread ON notifications(thread_id) WHERE thread_id IS NOT NULL;
CREATE INDEX idx_notifications_message ON notifications(message_id) WHERE message_id IS NOT NULL;
CREATE INDEX idx_schedule_runs_message ON schedule_runs(message_id) WHERE message_id IS NOT NULL;
CREATE INDEX idx_schedule_findings_run ON schedule_findings(run_id) WHERE run_id IS NOT NULL;
