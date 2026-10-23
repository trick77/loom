-- ListForProject filters artifacts by (user_id, project_id) and orders by
-- created_at; without an index it scanned all of the user's artifacts.
CREATE INDEX idx_artifacts_user_project ON artifacts(user_id, project_id, created_at);
