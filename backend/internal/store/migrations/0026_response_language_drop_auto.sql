-- Drop the removed 'auto' response-language value: reset existing rows to unset
-- (empty). New users are inserted with '' and the frontend seeds the browser
-- locale on first visit; the chat prompt now yields to the user's own language
-- when unset, so no row ever needs to force English. The column's DEFAULT 'auto'
-- becomes cosmetic — every INSERT supplies the value explicitly.
UPDATE users SET response_language = '' WHERE response_language = 'auto';
