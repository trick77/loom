-- Sessions moved into a signed cookie (kit/auth): the table is never read
-- again. Its indexes go with it.
DROP TABLE sessions;
