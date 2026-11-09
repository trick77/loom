-- unread_runs counts the schedule's runs with news since its news was last
-- read or the schedule last saved: enough of them in a row turn it off. A
-- count, not a query over notifications: news a newer run replaced is deleted.
ALTER TABLE schedules ADD COLUMN unread_runs INTEGER NOT NULL DEFAULT 0;
