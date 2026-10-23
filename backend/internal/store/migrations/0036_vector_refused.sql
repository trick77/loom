-- Chunks the embedding model refused outright (a bad request, e.g. past its
-- input limit) during re-embedding. Listed so later runs skip them instead of
-- retrying forever; cleared when the vector table is rebuilt (another model
-- may take them). The foreign key drops a row with its chunk, whichever path
-- deletes it: a chunk id can be reused, and a new chunk must not inherit the
-- refusal.
CREATE TABLE vector_refused (
    chunk_id INTEGER PRIMARY KEY REFERENCES chunks(id) ON DELETE CASCADE
);
