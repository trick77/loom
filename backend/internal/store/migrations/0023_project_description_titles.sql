-- The project description is now an auto-generated, one-sentence big-picture summary
-- of all the project's thread titles, regenerated as the project grows (gated on a
-- change in the titled-thread count plus the project-memory debounce) rather than a
-- one-shot fill from the early conversation.
--
-- description_user_edited locks a hand-edited description: once the user sets a
-- non-empty description, auto-generation never overwrites it. Emptying the description
-- clears this flag (re-arming auto-generation).
--
-- description_source_thread_count records the titled-thread count at the last
-- auto-generation; the refresh gate regenerates only when the current count differs.
--
-- Existing rows default to description_user_edited = 0 (auto-managed): existing
-- auto-generated descriptions are intentionally re-generated as big-picture summaries
-- on the next gate. The schema cannot reconstruct which past descriptions were
-- hand-edited, so a pre-existing manual edit (if any) would be replaced once; all
-- edits from now on are protected by the flag.
ALTER TABLE projects ADD COLUMN description_user_edited INTEGER NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN description_source_thread_count INTEGER NOT NULL DEFAULT 0;
