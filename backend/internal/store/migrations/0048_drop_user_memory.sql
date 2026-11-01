-- The old user memory blob is gone. 0.2.14 converts every user's blob into
-- user_facts on its first memory sweep (and writes their fact cursor); from
-- here on loom no longer reads it.
--
-- Refuse to drop a blob that was never converted: an instance upgrading from
-- 0.2.13 or older straight past 0.2.14 runs 0047 and 0048 in one boot, before
-- any sweep, and would lose every user's memory. The CHECK fails the migration
-- (and the boot) naming the fix: run 0.2.14 until its first sweep has run.
CREATE TEMP TABLE unconverted_user_memory (pending INTEGER CONSTRAINT upgrade_through_0_2_14_first CHECK (pending = 0));
INSERT INTO unconverted_user_memory
SELECT COUNT(*) FROM user_memory m
WHERE trim(m.content) <> ''
  AND NOT EXISTS (SELECT 1 FROM user_fact_cursor c WHERE c.user_id = m.user_id);
DROP TABLE unconverted_user_memory;

DROP TABLE user_memory;
