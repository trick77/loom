-- Persist the attachments a user sent with a message (uploaded images and
-- attached documents) so a sent message's previews survive a hard reload,
-- instead of living only in the client's ephemeral send state. Stored as a JSON
-- array mirroring the existing citations/artifacts columns; empty for messages
-- created before this migration or sent without any attachment.
ALTER TABLE messages ADD COLUMN attachments TEXT NOT NULL DEFAULT '[]';
