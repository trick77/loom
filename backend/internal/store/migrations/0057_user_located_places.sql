-- The town the browser's position named, set by the user on the start page
-- ("use my current location"). It holds over user_locations (read from the
-- memory, maybe stale or wrong) until the user locates again or drops it, and
-- is never a fact: derivations leave it alone. Only the town's centre is kept,
-- never the position. credit_* names the map data the name came from, shown
-- with it.
CREATE TABLE user_located_places (
    user_id TEXT PRIMARY KEY NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    region TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    timezone TEXT NOT NULL DEFAULT '',
    credit_text TEXT NOT NULL DEFAULT '',
    credit_url TEXT NOT NULL DEFAULT '',
    located_at TEXT NOT NULL DEFAULT (datetime('now'))
);
