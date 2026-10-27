-- A schedule no longer remembers what it reported: an item once announced
-- muted every later update on it. A run keeps the labels of the items it
-- reported itself; the task's own time span decides what is new.

ALTER TABLE schedule_runs ADD COLUMN labels TEXT NOT NULL DEFAULT '[]';

UPDATE schedule_runs
SET labels = (
    SELECT json_group_array(f.label ORDER BY f.first_seen_at, f.rowid) FROM schedule_findings f
    WHERE f.run_id = schedule_runs.id)
WHERE EXISTS (SELECT 1 FROM schedule_findings f WHERE f.run_id = schedule_runs.id);

DROP TABLE schedule_findings;
