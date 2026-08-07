-- Thread titles taken from the user's first prompt were stored verbatim, so most
-- read "why is the sky blue?" in the sidebar and the thread header. Those titles
-- are capitalized on write from now on; bring the existing rows in line.
--
-- Only rows whose title is still literally one of the thread's own user messages
-- are touched. A title the user typed by hand is theirs — "ffmpeg notes" must
-- stay "ffmpeg notes" — and nothing in the schema marks a title as user-chosen,
-- so matching the prompt is how a prompt-derived title is recognized. Titles the
-- normalizer reshaped (collapsed whitespace, stripped quotes) no longer match
-- their message and are left alone: under-reaching here is the safe direction.
--
-- SQLite's upper() is ASCII-only, so the guard restricts the rewrite to exactly
-- that range: an accented first letter is left as it is rather than mangled (the
-- Go normalizer capitalizes it on the next write). A second uppercase letter
-- means the lowercase first one is deliberate — "iPhone battery", "eBay export".
UPDATE threads
SET title = upper(substr(title, 1, 1)) || substr(title, 2)
WHERE substr(title, 1, 1) BETWEEN 'a' AND 'z'
  AND substr(title, 2, 1) NOT BETWEEN 'A' AND 'Z'
  AND EXISTS (
    SELECT 1
    FROM messages
    WHERE messages.thread_id = threads.id
      AND messages.role = 'user'
      AND messages.content = threads.title
  );
