// Package webtoken finds the developer token the music.apple.com web player
// ships in its JavaScript. Apple Music's API wants a developer token on every
// request; without a paid developer account the web player's own one is the
// only source. It is a JWT with issuer AMPWebPlay, valid for weeks, embedded in
// the player's index~<hash>.js bundle (checked 2026-11-01).
package webtoken

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the web player.
const DefaultBaseURL = "https://music.apple.com"

const (
	// pagePath is any player page; every one links the main bundle.
	pagePath = "/us/browse"
	// issuer marks the web player's token among the JWTs in the bundle.
	issuer = "AMPWebPlay"
	// refreshBefore renews a token this long before it expires, so a request
	// never goes out with one about to lapse.
	refreshBefore = 24 * time.Hour
	// minRecheck spaces out refetches when the bundle's best token is itself
	// inside refreshBefore, or the refetch failed: otherwise every call would
	// download the bundle again.
	minRecheck = time.Hour
	// failBackoff is how long a failed fetch, with no valid token to fall back
	// on, answers from memory before the page is fetched again: a broken
	// bundle must not be downloaded once per call.
	failBackoff = time.Minute
	// fetchTimeout bounds one fetch of page and bundle. It runs detached from
	// the caller that started it: callers queued behind it need its result.
	fetchTimeout = 45 * time.Second
	maxPageBytes = 8 << 20
	// maxBundleBytes: the bundle was 3.3 MB on 2026-11-01.
	maxBundleBytes = 32 << 20
)

var (
	bundleRe = regexp.MustCompile(`/assets/index[~-][A-Za-z0-9_-]+\.js`)
	jwtRe    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
)

// Errors name the step that failed, so a broken token hunt says where Apple
// changed the page.
var (
	ErrNoBundle = errors.New("webtoken: the player page links no index bundle")
	ErrNoToken  = errors.New("webtoken: the bundle holds no valid AMPWebPlay token")
)

// Token is a developer token and its expiry.
type Token struct {
	Value   string
	Expires time.Time
}

// Source hands out the current developer token, fetching a new one when the
// cached one nears its end or the API refused it. One fetch runs at a time.
type Source struct {
	BaseURL string
	HTTP    *http.Client
	// OnNew is told about every freshly fetched token, so it can be kept
	// across restarts. Optional.
	OnNew func(Token)
	Now   func() time.Time

	mu  sync.Mutex
	tok Token
	// recheck is when the cached token is looked for anew: refreshBefore ahead
	// of its expiry, at least minRecheck after the last look.
	recheck time.Time
	// failed and failedUntil remember a fetch that failed with no valid token
	// to fall back on: until then, Token answers with failed at once.
	failed      error
	failedUntil time.Time
}

// Seed installs a token kept from an earlier run.
func (s *Source) Seed(t Token) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tok, s.recheck = t, t.Expires.Add(-refreshBefore)
}

// Token returns a usable developer token.
func (s *Source) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	valid := s.tok.Value != "" && now.Before(s.tok.Expires)
	if valid && now.Before(s.recheck) {
		return s.tok.Value, nil
	}
	if !valid && s.failed != nil && now.Before(s.failedUntil) {
		return "", s.failed
	}
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout)
	defer cancel()
	t, err := s.fetch(fctx, now)
	if err != nil {
		if valid {
			// Still good for a while: keep it, look again later.
			s.recheck = now.Add(minRecheck)
			return s.tok.Value, nil
		}
		s.failed, s.failedUntil = err, now.Add(failBackoff)
		return "", err
	}
	s.failed = nil
	changed := t.Value != s.tok.Value
	s.tok, s.recheck = t, later(t.Expires.Add(-refreshBefore), now.Add(minRecheck))
	if changed && s.OnNew != nil {
		s.OnNew(t)
	}
	return t.Value, nil
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// Invalidate drops the cached token if it is still the one the API refused,
// so the next Token call fetches anew. A token another caller already
// replaced stays.
func (s *Source) Invalidate(refused string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tok.Value == refused {
		s.tok, s.recheck = Token{}, time.Time{}
	}
}

func (s *Source) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Source) fetch(ctx context.Context, now time.Time) (Token, error) {
	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	page, err := s.get(ctx, base+pagePath, maxPageBytes)
	if err != nil {
		return Token{}, fmt.Errorf("webtoken: player page: %w", err)
	}
	path := bundleRe.Find(page)
	if path == nil {
		return Token{}, ErrNoBundle
	}
	bundle, err := s.get(ctx, base+string(path), maxBundleBytes)
	if err != nil {
		return Token{}, fmt.Errorf("webtoken: bundle %s: %w", path, err)
	}
	return Extract(bundle, now)
}

func (s *Source) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// Extract picks the AMPWebPlay token with the latest expiry from a bundle,
// skipping ones already expired.
func Extract(bundle []byte, now time.Time) (Token, error) {
	var best Token
	for _, m := range jwtRe.FindAll(bundle, -1) {
		parts := strings.Split(string(m), ".")
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
		if err != nil {
			continue
		}
		var claims struct {
			Iss string `json:"iss"`
			Exp int64  `json:"exp"`
		}
		if json.Unmarshal(raw, &claims) != nil || claims.Iss != issuer {
			continue
		}
		exp := time.Unix(claims.Exp, 0)
		if !exp.After(now) || !exp.After(best.Expires) {
			continue
		}
		best = Token{Value: string(m), Expires: exp}
	}
	if best.Value == "" {
		return Token{}, ErrNoToken
	}
	return best, nil
}
