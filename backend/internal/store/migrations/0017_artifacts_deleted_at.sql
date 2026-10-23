-- Soft-delete marker for artifacts. Deleting an artifact from the Artifacts
-- library removes its bytes from disk but keeps the row so the chat message that
-- referenced it can still render a tombstone card ("this file was deleted").
-- NULL means the artifact is live; a timestamp marks when it was deleted.
-- Mirrors the archived_at soft-flag pattern used by threads and projects.
ALTER TABLE artifacts ADD COLUMN deleted_at TEXT;
