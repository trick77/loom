-- The chat slot a thread's turns run on: "standard" (the deployment's chat
-- model) or "deep" (its optional think-longer model). Sticky per thread, picked
-- in the composer. A slot the deployment no longer configures falls back to
-- standard at turn time, so the column never needs rewriting.
ALTER TABLE threads ADD COLUMN chat_slot TEXT NOT NULL DEFAULT 'standard';
