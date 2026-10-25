-- Every scheduled run starts its own thread: the run records its thread, the
-- schedule keeps none. A run's thread is an ordinary thread the user follows
-- up in, so "Continue in new thread" and its branch links go.

ALTER TABLE schedule_runs ADD COLUMN thread_id TEXT REFERENCES threads(id) ON DELETE SET NULL;

-- A run before this one wrote into its schedule's shared thread.
UPDATE schedule_runs
SET thread_id = COALESCE(
    (SELECT m.thread_id FROM messages m WHERE m.id = schedule_runs.message_id),
    (SELECT s.thread_id FROM schedules s WHERE s.id = schedule_runs.schedule_id));

CREATE INDEX idx_schedule_runs_thread ON schedule_runs(thread_id) WHERE thread_id IS NOT NULL;

DROP INDEX idx_schedules_thread;
ALTER TABLE schedules DROP COLUMN thread_id;

DROP INDEX idx_threads_branched_thread;
DROP INDEX idx_threads_branched_message;
ALTER TABLE threads DROP COLUMN branched_from_thread_id;
ALTER TABLE threads DROP COLUMN branched_from_message_id;
