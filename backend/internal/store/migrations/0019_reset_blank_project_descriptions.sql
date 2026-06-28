-- Re-arm auto-description for projects that are stuck with a blank description but a
-- set auto_description_generated_at marker. This state was reachable when a project's
-- auto-generated description was later cleared (a project edit that blanked the
-- description) without clearing the marker: auto-generation guards on
-- auto_description_generated_at IS NULL, so it never regenerated and the description
-- stayed empty permanently. UpdateProject now clears the marker on blanking, and the
-- MemoryWorker backfills empty descriptions independently of the activity gate; this
-- one-shot pass recovers projects already in the stuck state so they self-heal on the
-- next memory sweep.
UPDATE projects
SET auto_description_generated_at = NULL
WHERE description = '' AND auto_description_generated_at IS NOT NULL;
