-- Chunks the embedding model refused outright (a bad request, e.g. past its
-- input limit) during re-embedding. Listed so later runs skip them instead of
-- retrying forever; cleared when the vector table is rebuilt (another model
-- may take them) and when the chunk is deleted (its id can be reused).
CREATE TABLE vector_refused (
    chunk_id INTEGER PRIMARY KEY
);
