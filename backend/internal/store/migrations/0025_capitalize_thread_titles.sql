-- Thread titles taken from the user's first prompt were stored verbatim, so most
-- read "why is the sky blue?" in the sidebar and the thread header. Titles are
-- capitalized on write from now on; bring the existing rows in line.
--
-- SQLite's upper() is ASCII-only, so the guard restricts the rewrite to exactly
-- that range: an accented first letter is left as it is rather than mangled (the
-- Go normalizer capitalizes it on the next write). A second uppercase letter means
-- the lowercase first one is deliberate — "iPhone battery", "eBay export".
UPDATE threads
SET title = upper(substr(title, 1, 1)) || substr(title, 2)
WHERE substr(title, 1, 1) BETWEEN 'a' AND 'z'
  AND substr(title, 2, 1) NOT BETWEEN 'A' AND 'Z';
