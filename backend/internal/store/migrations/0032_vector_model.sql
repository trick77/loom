-- The embedding model that wrote the vectors in vec_chunks. Two models of the
-- same width still produce incompatible vectors, so boot compares this with
-- the configured model and rebuilds on a change, not only on a width change.
-- One row at most; empty until the first boot records the configured model.
CREATE TABLE vector_model (
    id    INTEGER PRIMARY KEY CHECK (id = 1),
    model TEXT NOT NULL
);
