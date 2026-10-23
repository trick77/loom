-- Cache a document's extracted plain text on the row, so the inline/full-document
-- paths can read it directly instead of re-running Tika extraction on every chat
-- turn. Populated at ingestion (Ingest); empty for documents indexed before this
-- migration or never indexed, which fall back to live extraction from the volume.
ALTER TABLE documents ADD COLUMN full_text TEXT NOT NULL DEFAULT '';
