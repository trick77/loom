-- Lifetime count of run_python sandbox jobs, next to the other tool counters.
ALTER TABLE user_usage_totals ADD COLUMN code_runs INTEGER NOT NULL DEFAULT 0;
