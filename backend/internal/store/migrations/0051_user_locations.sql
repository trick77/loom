-- Where the user is, derived from their memory (user_facts) by a gate call and
-- turned into coordinates by the weather sidecar's lookup: a home place and an
-- optional current one that holds through `until` (a date in the user's zone).
-- Only a town's centre is kept, never a precise position. Nothing is guessed
-- from a timezone or an IP: no fact, no place.
CREATE TABLE user_locations (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('home', 'current')),
    name TEXT NOT NULL,
    region TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    timezone TEXT NOT NULL DEFAULT '',
    until TEXT,
    fact_id TEXT,
    PRIMARY KEY (user_id, kind)
);

-- The active facts the places were derived from, as a hash: facts that hash
-- otherwise (added, changed, forgotten, seen again on another day) derive them
-- anew. No hook on every path that changes a fact is needed.
CREATE TABLE user_location_state (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    facts_hash TEXT NOT NULL,
    resolved_at TEXT NOT NULL DEFAULT (datetime('now'))
);
