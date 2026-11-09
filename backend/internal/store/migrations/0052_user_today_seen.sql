-- The day (YYYY-MM-DD, the user's zone) the user last opened the start page's
-- "today" view: the first visit of a day shows the weather as tiles, later
-- ones as one line. On the user row so every device agrees.
ALTER TABLE users ADD COLUMN today_seen_on TEXT NOT NULL DEFAULT '';
