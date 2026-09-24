-- The thread list orders by COALESCE(last_message_at, updated_at) DESC, which
-- idx_threads_user_recent (on the raw columns) cannot serve: every page sorted
-- all of the user's threads in a temporary b-tree. An index on the expression
-- itself, in the list's exact (user, archived, activity, updated, id) order,
-- lets the page come straight off the index.
CREATE INDEX idx_threads_user_recency
    ON threads(user_id, archived_at, COALESCE(last_message_at, updated_at) DESC, updated_at DESC, id DESC);
