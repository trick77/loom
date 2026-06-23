-- Prompt classifier: per-thread category chosen on the first message (alongside
-- the title) and used to inject a category-specific instruction block into the
-- system prompt. Empty string means unclassified (no block injected).
ALTER TABLE threads ADD COLUMN category TEXT NOT NULL DEFAULT '';
