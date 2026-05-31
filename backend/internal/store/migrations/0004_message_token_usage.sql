ALTER TABLE messages ADD COLUMN prompt_tokens INTEGER;
ALTER TABLE messages ADD COLUMN completion_tokens INTEGER;
ALTER TABLE messages ADD COLUMN total_tokens INTEGER;
ALTER TABLE messages ADD COLUMN cached_tokens INTEGER;
ALTER TABLE messages ADD COLUMN reasoning_tokens INTEGER;
