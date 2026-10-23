-- The id the client gave a send. A client whose connection dropped before the
-- server confirmed the message looks for it to learn whether it was stored.
ALTER TABLE messages ADD COLUMN client_message_id TEXT NOT NULL DEFAULT '';
