-- Large text blocks the user pasted into the composer, collapsed into "Pasted"
-- chips instead of flooding the message bubble. Stored purely for rendering: the
-- block text is also folded into `content` so the model sees it unchanged, but the
-- bubble strips it out and renders a chip. A JSON array of {text, lineCount};
-- "[]" for messages sent without a collapsed paste.
ALTER TABLE messages ADD COLUMN pasted_texts TEXT NOT NULL DEFAULT '[]';
