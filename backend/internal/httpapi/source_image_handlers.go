package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// handleSourceImage proxies a source's preview image (an MCP source's
// thumbnail) so the reader's browser never contacts the image host itself:
// it would learn the reader's IP and which sources they opened, and a
// misbehaving server could use the URL as a tracking beacon. It shares the
// favicon machinery: the SSRF-guarded client, the raster-only/size-capped
// fetch, the on-disk cache and the locked-down response headers. Keyed by
// the full URL, since one host serves many different thumbnails.
func (s *server) handleSourceImage(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	target, err := url.Parse(raw)
	if raw == "" || err != nil || target.Scheme != "https" || target.Host == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid url")
		return
	}
	key := faviconCacheKey("source-image\x00" + raw)
	etag := faviconETag(key)
	if path, ct, ok := s.faviconCached(key); ok {
		serveFaviconFile(w, r, path, ct, etag)
		return
	}
	gate := faviconAcquire(key)
	defer faviconRelease(key, gate)
	if path, ct, ok := s.faviconCached(key); ok {
		serveFaviconFile(w, r, path, ct, etag)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), faviconFetchTimeout)
	defer cancel()
	body, ct, err := s.fetchFaviconBytes(ctx, raw)
	if err != nil {
		writeFaviconMissError(w)
		return
	}
	if path, err := s.writeFaviconCache(key, body, ct); err == nil {
		serveFaviconFile(w, r, path, ct, etag)
		return
	}
	// No cache directory (or a failed write): serve the bytes once, with the
	// same defensive headers serveFaviconFile sets.
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", faviconCacheControl)
	_, _ = w.Write(body) //nolint:gosec // G705: fetchFaviconBytes admits raster image/* only (no SVG), served with nosniff and a sandboxing CSP
}
