-- Per-thread image-generation model lock: the model chosen for the first image
-- generated in a thread (e.g. "flux-2-klein-4b" or the typography model
-- "flux-2-flex") is recorded here and reused for every later image in the thread,
-- so the model never flip-flops mid-conversation. Empty string means no image has
-- been generated yet (not locked).
ALTER TABLE threads ADD COLUMN image_model TEXT NOT NULL DEFAULT '';
