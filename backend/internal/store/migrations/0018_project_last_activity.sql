-- last_activity_at tracks the last *user* activity in a project: a new message in
-- any of its threads, a new thread, or a user edit to the project name/description.
-- It drives the project card's "Updated X ago" label and the "Recent activity" sort,
-- so it must NOT move on incidental events (star, archive, auto-description, sharing).
--
-- Added nullable with no default: SQLite rejects a parenthesized-expression default
-- (DEFAULT (datetime('now'))) in ALTER TABLE ADD COLUMN. The column is backfilled
-- unconditionally below so no row stays NULL, and CreateProject sets it explicitly.
ALTER TABLE projects ADD COLUMN last_activity_at TEXT;

-- Pass 1: every existing row gets a value.
UPDATE projects SET last_activity_at = updated_at;

-- Pass 2: raise to the latest thread activity where it is newer than the row's own
-- updated_at, so projects with recent conversations sort by real activity.
UPDATE projects SET last_activity_at = (
    SELECT MAX(t.last_message_at) FROM threads t
    WHERE t.project_id = projects.id AND t.last_message_at IS NOT NULL)
WHERE EXISTS (
    SELECT 1 FROM threads t
    WHERE t.project_id = projects.id AND t.last_message_at > projects.last_activity_at);

DROP INDEX IF EXISTS idx_projects_user_active;
CREATE INDEX idx_projects_user_activity ON projects(user_id, archived_at, last_activity_at DESC);
