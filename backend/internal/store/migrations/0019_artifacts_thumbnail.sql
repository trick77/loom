-- Sidecar thumbnail path for image artifacts. NULL means no thumbnail has been
-- generated yet (a non-raster artifact such as an SVG, or a raster artifact that
-- predates this feature / failed generation). Raster image artifacts get a small
-- JPEG written into the reserved .loom/thumbnails subtree (mirroring the original's
-- relpath); the value here is that thumbnail's volume-relative path. The thumbnail
-- endpoint lazily generates and backfills this column on first view for older
-- artifacts.
ALTER TABLE artifacts ADD COLUMN thumbnail_relpath TEXT;
