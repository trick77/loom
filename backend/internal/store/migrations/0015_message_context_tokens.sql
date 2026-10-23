-- context_tokens holds the final answer call's model-reported total_tokens for an
-- assistant message: the real size of that single generation's context (input +
-- output). Unlike total_tokens, which sums usage across every model call in the
-- turn (each tool round re-counts the growing prompt, plus background helpers),
-- this is a single model-reported figure, so it is the correct basis for "how full
-- is the context window". Nullable: pre-existing messages never recorded it and
-- render no context percentage rather than a wrong one.
ALTER TABLE messages ADD COLUMN context_tokens INTEGER;
