package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// Cache-Control for a proxied source image. Success is cached by the browser
// for the favicon week; a failure only briefly, since a slow or flaky
// upstream proves nothing about the image.
const (
	sourceImageCacheControl     = faviconCacheControl
	sourceImageMissCacheControl = "private, max-age=300"
)

// handleSourceImage proxies a source's preview image (an MCP source's
// thumbnail) so the reader's browser never contacts the image host itself:
// it would learn the reader's IP and which sources they opened, and a
// misbehaving server could use the URL as a tracking beacon. It reuses the
// favicon fetch — the SSRF-guarded client, raster image/* only, size cap —
// but keeps nothing on disk: the key is a whole URL, so a signed-in caller
// could otherwise grow the cache without bound one query string at a time.
// The browser caches the response instead.
func (s *server) handleSourceImage(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	target, err := url.Parse(raw)
	if raw == "" || err != nil || target.Scheme != "https" || target.Host == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid url")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), faviconFetchTimeout)
	defer cancel()
	body, ct, err := s.fetchFaviconBytes(ctx, raw)
	if err != nil {
		w.Header().Set("Cache-Control", sourceImageMissCacheControl)
		writeJSONError(w, http.StatusBadGateway, "image unavailable")
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	// Bytes from a third party served same-origin: no sniffing, and no active
	// content even if loaded outside an <img> (see serveFaviconFile).
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", sourceImageCacheControl)
	_, _ = w.Write(body) //nolint:gosec // G705: fetchFaviconBytes admits raster image/* only (no SVG), served with nosniff and a sandboxing CSP
}
