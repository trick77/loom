-- cost_nano_usd is what a turn cost at the vendor's published list rate, in
-- integer nano-USD, priced per model call by llmwire and summed over every call
-- the turn made (answer, tool rounds, title and reasoning helpers), the same
-- scope as total_tokens. It is a comparable equivalent, not an invoice: a call
-- billed in plan credits has no dollar figure the vendor reports. NULL means
-- unpriced (no rate for the model, or a turn from before this column), never 0:
-- zero would read as free.
ALTER TABLE messages ADD COLUMN cost_nano_usd INTEGER;
-- The user's lifetime figure across chat and embedding calls, priced calls only.
ALTER TABLE user_usage_totals ADD COLUMN cost_nano_usd INTEGER NOT NULL DEFAULT 0;
