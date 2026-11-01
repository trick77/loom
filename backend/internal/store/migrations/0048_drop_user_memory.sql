-- The old user memory blob is gone: every user's blob was converted into
-- user_facts on the first sweep after 0047, and loom no longer reads it.

DROP TABLE user_memory;
