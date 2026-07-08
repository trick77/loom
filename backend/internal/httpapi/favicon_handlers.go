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
	// refresh a stale favicon, delete its file from the cache dir.
	faviconCacheControl = "public, max-age=604800"
	faviconMaxBytes     = 256 << 10 // 256 KiB — favicons are tiny; cap abuse.
	faviconFetchTimeout = 5 * time.Second
	faviconUserAgent    = "loom-favicon-cache/1.0"
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
// their double-check. Distinct favicon urls are bounded by the number of cited
// sites, so the map does not grow without bound in practice.
var (
	faviconLocksMu sync.Mutex
	faviconLocks   = map[string]*sync.Mutex{}
)

func faviconLock(key string) *sync.Mutex {
	faviconLocksMu.Lock()
	defer faviconLocksMu.Unlock()
	m, ok := faviconLocks[key]
	if !ok {
		m = &sync.Mutex{}
		faviconLocks[key] = m
	}
	return m
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

// handleFavicon proxies and caches a single favicon image. The frontend routes
// each entry of its fallback chain (tool-provided url, then Google's favicon
// service) through here, so both hit a shared on-disk cache and re-renders serve
// from the browser cache instead of re-fetching third parties. On any failure it
// returns a non-2xx status so the <img>'s onError advances the chain unchanged.
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

	key := faviconCacheKey(raw)
	etag := faviconETag(key)

	if path, ct, ok := s.faviconCached(key); ok {
		serveFaviconFile(w, r, path, ct, etag)
		return
	}

	// Cache miss: serialize same-key fetches, then double-check the cache.
	lock := faviconLock(key)
	lock.Lock()
	defer lock.Unlock()
	if path, ct, ok := s.faviconCached(key); ok {
		serveFaviconFile(w, r, path, ct, etag)
		return
	}

	path, ct, status, err := s.fetchAndCacheFavicon(r.Context(), key, raw)
	if err != nil {
		if status == 0 {
			status = http.StatusBadGateway
		}
		writeJSONError(w, status, "favicon fetch failed")
		return
	}
	serveFaviconFile(w, r, path, ct, etag)
}

// fetchAndCacheFavicon downloads the favicon at rawURL, validates it is a small
// image, and persists the bytes (plus a .ct sidecar holding its content-type)
// atomically. status carries an HTTP status to surface to the caller on error.
func (s *server) fetchAndCacheFavicon(ctx context.Context, key, rawURL string) (path, contentType string, status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", http.StatusBadRequest, err
	}
	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", faviconUserAgent)

	client := s.faviconClient
	if client == nil {
		client = faviconDefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", http.StatusBadGateway, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", resp.StatusCode, fmt.Errorf("upstream status %d", resp.StatusCode)
	}

	ct := strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0])
	if !strings.HasPrefix(ct, "image/") {
		return "", "", http.StatusUnsupportedMediaType, fmt.Errorf("non-image content-type %q", ct)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, faviconMaxBytes+1))
	if err != nil {
		return "", "", http.StatusBadGateway, err
	}
	if len(body) == 0 {
		return "", "", http.StatusBadGateway, errors.New("empty favicon")
	}
	if len(body) > faviconMaxBytes {
		return "", "", http.StatusRequestEntityTooLarge, errors.New("favicon too large")
	}

	dir, err := s.faviconDir()
	if err != nil {
		return "", "", http.StatusInternalServerError, err
	}
	dataPath := filepath.Join(dir, key)
	if err := faviconWriteAtomic(dataPath, body); err != nil {
		return "", "", http.StatusInternalServerError, err
	}
	if err := faviconWriteAtomic(dataPath+".ct", []byte(ct)); err != nil {
		return "", "", http.StatusInternalServerError, err
	}
	return dataPath, ct, http.StatusOK, nil
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

func faviconCacheKey(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
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
