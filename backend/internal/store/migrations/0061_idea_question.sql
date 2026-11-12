-- The question the captured answer replied to: the user's own message before
-- it, taken by the server when the idea is saved (never sent by the client),
-- without pasted blocks. '' for an idea noted without a message.
ALTER TABLE ideas ADD COLUMN question TEXT NOT NULL DEFAULT '';
