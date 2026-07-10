package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// faviconCacheControl lets browsers cache proxied favicons for a week. After that
	// the browser revalidates via the ETag, but the server never re-fetches upstream:
	// the on-disk entry is permanent, so a 304 just re-serves the same bytes. To
	// refresh a stale favicon, delete its file from the cache dir. There is no
	// eviction, so the cache dir grows with the number of distinct favicon urls ever
	// requested (each capped at faviconMaxBytes); the route is auth-gated, so this is
	// an accepted trade-off rather than an open-ended surface. Add LRU eviction if the
	// dir is ever observed to grow unreasonably.
	faviconCacheControl = "public, max-age=604800"
	faviconMaxBytes     = 256 << 10 // 256 KiB — favicons are tiny; cap abuse.
	faviconFetchTimeout = 5 * time.Second
	// faviconResolveTimeout bounds a whole resolve (page fetch + candidate probes)
	// so a slow site can't tie the request up for one 5s timeout per candidate.
	faviconResolveTimeout = 12 * time.Second
	faviconUserAgent      = "loom-favicon-cache/1.0"
)

// faviconDefaultClient fetches favicon bytes with a short timeout and an SSRF guard
// that refuses to connect to private/loopback/link-local addresses. The guard runs
// at dial time (post-DNS-resolution), so it also defeats DNS-rebinding. No proxy is
// configured on purpose: a proxy would dial the proxy's IP and bypass the guard.
// s.faviconClient overrides it in tests (whose upstreams live on loopback).
var faviconDefaultClient = &http.Client{
	Timeout: faviconFetchTimeout,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: faviconFetchTimeout,
			Control: guardPublicAddr,
		}).DialContext,
	},
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// faviconLocks serializes concurrent fetches of the same url so a burst of
// identical requests hits the network once; the losers find the file on disk on
// their double-check. Because the endpoint accepts an arbitrary url, the map is
// kept bounded by ref-counting: an entry is dropped once its last in-flight
// waiter releases it, so only keys with active requests are held. Correctness
// never depends on the lock (the on-disk double-check and atomic write make
// concurrent fetches of the same key harmless) — it is purely stampede control.
type faviconGate struct {
	mu   sync.Mutex
	refs int
}

var (
	faviconLocksMu sync.Mutex
	faviconLocks   = map[string]*faviconGate{}
)

// faviconAcquire returns the (locked) gate for key, registering a reference.
func faviconAcquire(key string) *faviconGate {
	faviconLocksMu.Lock()
	g, ok := faviconLocks[key]
	if !ok {
		g = &faviconGate{}
		faviconLocks[key] = g
	}
	g.refs++
	faviconLocksMu.Unlock()
	g.mu.Lock()
	return g
}

// faviconRelease unlocks the gate and evicts it from the map when no waiters remain.
func faviconRelease(key string, g *faviconGate) {
	g.mu.Unlock()
	faviconLocksMu.Lock()
	g.refs--
	if g.refs == 0 {
		delete(faviconLocks, key)
	}
	faviconLocksMu.Unlock()
}

// guardPublicAddr rejects connections to non-public IP ranges. IsPrivate covers
// RFC1918 (v4) and RFC4193 ULA (fc00::/7, v6).
func guardPublicAddr(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("favicon: unresolved address %q", address)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return fmt.Errorf("favicon: refusing to connect to %s", ip)
	}
	return nil
}

// handleFavicon resolves, proxies and caches the best icon for a web source. The
// query "u" is the source's page URL; the backend resolves the site's best icon
// (apple-touch-icon / largest raster / favicon.ico / a rendered service icon — see
// resolveIconCandidates) rather than trusting a single tool-provided favicon URL,
// which is often a dark, light-mode-only glyph that vanishes on the dark UI. The
// resolved bytes are cached on disk keyed by host and served with a long browser
// Cache-Control, so re-renders and reloads serve from cache instead of re-resolving
// or re-fetching third parties. On failure it returns a non-2xx so the <img>'s
// onError falls back to the letter avatar.
func (s *server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	if raw == "" {
		writeJSONError(w, http.StatusBadRequest, "missing url")
		return
	}
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "https" && target.Scheme != "http") || target.Host == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid url")
		return
	}
	scheme := strings.ToLower(target.Scheme)
	// Authority (host[:port]) — the identity for caching and for building the site's
	// candidate icon URLs. Real sources use the default port, so this is just the
	// hostname in practice; keeping any explicit port keeps non-standard ports correct.
	host := strings.ToLower(target.Host)

	// Cache and serialize by host: one site shows one icon, so all its pages/paths
	// share a single resolved-and-cached icon.
	key := faviconCacheKey(host)
	etag := faviconETag(key)
	if path, ct, ok := s.faviconCached(key); ok {
		serveFaviconFile(w, r, path, ct, etag)
		return
	}

	// Cache miss: serialize same-host resolves, then double-check the cache. The gate
	// is released before serving so a slow client never blocks other same-host
	// requests — by then the bytes are on disk.
	gate := faviconAcquire(key)
	if path, ct, ok := s.faviconCached(key); ok {
		faviconRelease(key, gate)
		serveFaviconFile(w, r, path, ct, etag)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), faviconResolveTimeout)
	defer cancel()
	path, ct, err := s.resolveAndCacheIcon(ctx, key, scheme, host, raw)
	faviconRelease(key, gate)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "favicon resolve failed")
		return
	}
	serveFaviconFile(w, r, path, ct, etag)
}

// resolveAndCacheIcon tries each candidate icon for the site (best-first) and
// persists the first one that is a valid small raster image under the host key,
// returning its on-disk path and content-type. It errors only when no candidate
// yields a usable icon.
func (s *server) resolveAndCacheIcon(ctx context.Context, key, scheme, host, pageURL string) (path, contentType string, err error) {
	for _, candidate := range s.resolveIconCandidates(ctx, scheme, host, pageURL) {
		body, ct, ferr := s.fetchFaviconBytes(ctx, candidate)
		if ferr != nil {
			continue
		}
		p, werr := s.writeFaviconCache(key, body, ct)
		if werr != nil {
			return "", "", werr
		}
		return p, ct, nil
	}
	return "", "", errors.New("no usable icon")
}

// fetchFaviconBytes downloads a single candidate icon URL and validates it is a
// small raster image, returning its bytes and content-type. SVG is rejected: it can
// carry <script>, and we re-serve favicon bytes from our own origin. serveFaviconFile
// adds nosniff + a locked-down CSP as defence in depth for the raster types we serve.
func (s *server) fetchFaviconBytes(ctx context.Context, rawURL string) (body []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", faviconUserAgent)

	client := s.faviconClient
	if client == nil {
		client = faviconDefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	if !strings.HasPrefix(ct, "image/") || ct == "image/svg+xml" {
		return nil, "", fmt.Errorf("unsupported content-type %q", ct)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, faviconMaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", errors.New("empty favicon")
	}
	if len(data) > faviconMaxBytes {
		return nil, "", errors.New("favicon too large")
	}
	return data, ct, nil
}

// writeFaviconCache persists icon bytes (plus a .ct sidecar holding the content-type)
// under key, atomically, and returns the data file path.
func (s *server) writeFaviconCache(key string, body []byte, contentType string) (path string, err error) {
	dir, err := s.faviconDir()
	if err != nil {
		return "", err
	}
	dataPath := filepath.Join(dir, key)
	if err := faviconWriteAtomic(dataPath, body); err != nil {
		return "", err
	}
	if err := faviconWriteAtomic(dataPath+".ct", []byte(contentType)); err != nil {
		return "", err
	}
	return dataPath, nil
}

// faviconCached returns the on-disk path and content-type when both the image and
// its .ct sidecar are present.
func (s *server) faviconCached(key string) (path, contentType string, ok bool) {
	if s.faviconCacheDir == "" {
		return "", "", false
	}
	dataPath := filepath.Join(s.faviconCacheDir, key)
	if _, err := os.Stat(dataPath); err != nil {
		return "", "", false
	}
	ct, err := os.ReadFile(dataPath + ".ct")
	if err != nil {
		return "", "", false
	}
	return dataPath, strings.TrimSpace(string(ct)), true
}

// faviconDir returns the cache directory, creating it on first use.
func (s *server) faviconDir() (string, error) {
	if s.faviconCacheDir == "" {
		return "", errors.New("favicon cache disabled")
	}
	if err := os.MkdirAll(s.faviconCacheDir, 0o755); err != nil {
		return "", err
	}
	return s.faviconCacheDir, nil
}

func serveFaviconFile(w http.ResponseWriter, r *http.Request, path, contentType, etag string) {
	f, err := os.Open(path)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	// Defence in depth for bytes fetched from a third party and served same-origin:
	// forbid MIME sniffing, and sandbox any active content so a mislabeled/crafted
	// payload cannot execute in the app's origin if loaded outside an <img>.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", faviconCacheControl)
	w.Header().Set("ETag", etag)
	http.ServeContent(w, r, path, info.ModTime(), f)
}

// faviconWriteAtomic writes data to path via a temp file + rename so a concurrent
// reader never observes a partial file.
func faviconWriteAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fav-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// faviconCacheKey hashes a cache identity (the source host) into a filesystem-safe
// cache filename.
func faviconCacheKey(host string) string {
	sum := sha256.Sum256([]byte(host))
	return hex.EncodeToString(sum[:])
}

func faviconETag(key string) string {
	return fmt.Sprintf("%q", "fav-"+key)
}

// faviconCacheDirFor derives the shared (non-per-user) favicon cache directory as a
// sibling of the users dir. Empty usersDir disables on-disk caching.
func faviconCacheDirFor(usersDir string) string {
	if usersDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(usersDir), "favicons")
}
