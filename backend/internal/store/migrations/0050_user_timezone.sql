-- The IANA timezone the user's browser last sent (X-Loom-Timezone). It was held
-- in memory only, so a restart lost it until the next request: a queued turn or
-- a schedule made from chat read UTC meanwhile. The start page's "today" and
-- its sunrise/sunset need it too.
ALTER TABLE users ADD COLUMN timezone TEXT NOT NULL DEFAULT '';
