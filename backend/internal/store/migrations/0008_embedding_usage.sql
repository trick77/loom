ALTER TABLE user_usage_totals ADD COLUMN embedding_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE user_usage_totals ADD COLUMN embedding_requests INTEGER NOT NULL DEFAULT 0;
