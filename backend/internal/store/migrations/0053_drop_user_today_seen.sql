-- The start page shows the day's tiles on every visit now (and tomorrow's in
-- the evening): the first visit of a day no longer needs remembering.
ALTER TABLE users DROP COLUMN today_seen_on;
