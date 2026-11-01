package webtoken

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func jwt(iss string, exp time.Time) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"typ":"JWT","alg":"ES256","kid":"WebPlayKid"}`)) + "." +
		enc([]byte(fmt.Sprintf(`{"iss":%q,"iat":1,"exp":%d}`, iss, exp.Unix()))) + "." + enc([]byte("sig"))
}

func TestExtract_picksTheLatestValidWebPlayToken(t *testing.T) {
	old := jwt(issuer, now.Add(10*24*time.Hour))
	best := jwt(issuer, now.Add(60*24*time.Hour))
	bundle := strings.Join([]string{
		`var a="` + jwt("SomeoneElse", now.Add(90*24*time.Hour)) + `";`,
		`var b="` + jwt(issuer, now.Add(-time.Hour)) + `";`, // expired
		`var c="` + old + `";`,
		`var d="` + best + `";`,
		`var e="eyJnot.a.jwt";`,
	}, "\n")
	got, err := Extract([]byte(bundle), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != best {
		t.Errorf("token = %q, want the latest", got.Value)
	}
	if !got.Expires.Equal(now.Add(60 * 24 * time.Hour).Truncate(time.Second)) {
		t.Errorf("expires = %v", got.Expires)
	}
}

func TestExtract_noToken(t *testing.T) {
	if _, err := Extract([]byte(`var x = "`+jwt("Other", now.Add(time.Hour))+`"`), now); !errors.Is(err, ErrNoToken) {
		t.Fatalf("err = %v, want ErrNoToken", err)
	}
}

// player serves a page linking a bundle that holds token.
func player(t *testing.T, token string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /us/browse", func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = fmt.Fprint(w, `<html><script type="module" src="/assets/index~cc245af156.js"></script></html>`)
	})
	mux.HandleFunc("GET /assets/index~cc245af156.js", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `const t="%s";`, token)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSource_fetchesCachesAndReportsNewTokens(t *testing.T) {
	tok := jwt(issuer, now.Add(30*24*time.Hour))
	var hits atomic.Int32
	srv := player(t, tok, &hits)
	var reported []Token
	s := &Source{BaseURL: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }, OnNew: func(t Token) { reported = append(reported, t) }}
	for range 3 {
		got, err := s.Token(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if got != tok {
			t.Fatalf("token = %q", got)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("page fetched %d times, want 1 (cached)", hits.Load())
	}
	if len(reported) != 1 || reported[0].Value != tok {
		t.Errorf("OnNew got %v", reported)
	}
}

func TestSource_refetchesNearExpiryAndAfterInvalidate(t *testing.T) {
	tok := jwt(issuer, now.Add(30*24*time.Hour))
	var hits atomic.Int32
	srv := player(t, tok, &hits)
	s := &Source{BaseURL: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }}

	s.Seed(Token{Value: "seeded", Expires: now.Add(12 * time.Hour)}) // inside refreshBefore
	if got, _ := s.Token(context.Background()); got != tok {
		t.Fatalf("near-expiry seed was used: %q", got)
	}
	s.Invalidate("some other token") // not ours: stays
	_, _ = s.Token(context.Background())
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
	s.Invalidate(tok)
	_, _ = s.Token(context.Background())
	if hits.Load() != 2 {
		t.Errorf("hits = %d, want 2 after invalidate", hits.Load())
	}
}

// The bundle's best token is itself inside refreshBefore: it is used, and the
// bundle is not downloaded again on every call, only after minRecheck.
func TestSource_nearExpiryBundleTokenIsNotRefetchedPerCall(t *testing.T) {
	tok := jwt(issuer, now.Add(6*time.Hour))
	var hits atomic.Int32
	srv := player(t, tok, &hits)
	clock := now
	var reported int
	s := &Source{BaseURL: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return clock }, OnNew: func(Token) { reported++ }}
	for range 5 {
		if got, err := s.Token(context.Background()); err != nil || got != tok {
			t.Fatalf("token %q, %v", got, err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
	clock = clock.Add(minRecheck + time.Minute)
	_, _ = s.Token(context.Background())
	if hits.Load() != 2 || reported != 1 {
		t.Errorf("hits = %d (want 2), OnNew %d times (want 1: the token did not change)", hits.Load(), reported)
	}
}

// A failed refetch keeps a token that is still valid, and waits before trying
// again.
func TestSource_failedRefetchKeepsAValidToken(t *testing.T) {
	var hits atomic.Int32
	srv := player(t, "no token here", &hits)
	s := &Source{BaseURL: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }}
	s.Seed(Token{Value: "kept", Expires: now.Add(2 * time.Hour)})
	for range 3 {
		if got, err := s.Token(context.Background()); err != nil || got != "kept" {
			t.Fatalf("token %q, %v", got, err)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("hits = %d, want 1", hits.Load())
	}
}

// With no token to fall back on, a failed fetch is remembered for
// failBackoff: calls in between get the error without a download.
func TestSource_failedFetchBacksOff(t *testing.T) {
	var hits atomic.Int32
	srv := player(t, "no token here", &hits)
	clock := now
	s := &Source{BaseURL: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return clock }}
	for range 3 {
		if _, err := s.Token(context.Background()); !errors.Is(err, ErrNoToken) {
			t.Fatalf("err = %v", err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1", hits.Load())
	}
	clock = clock.Add(failBackoff + time.Second)
	_, _ = s.Token(context.Background())
	if hits.Load() != 2 {
		t.Errorf("hits = %d after the backoff, want 2", hits.Load())
	}
}

// A canceled first caller does not abort the fetch the others wait on.
func TestSource_fetchIgnoresTheCallersCancel(t *testing.T) {
	tok := jwt(issuer, now.Add(30*24*time.Hour))
	var hits atomic.Int32
	srv := player(t, tok, &hits)
	s := &Source{BaseURL: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := s.Token(ctx); err != nil || got != tok {
		t.Fatalf("token %q, %v", got, err)
	}
}

func TestSource_seededTokenIsUsed(t *testing.T) {
	s := &Source{BaseURL: "http://127.0.0.1:1", Now: func() time.Time { return now }}
	s.Seed(Token{Value: "kept", Expires: now.Add(10 * 24 * time.Hour)})
	if got, err := s.Token(context.Background()); err != nil || got != "kept" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestSource_errorsNameTheStep(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /us/browse", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, "<html></html>") })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	s := &Source{BaseURL: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }}
	if _, err := s.Token(context.Background()); !errors.Is(err, ErrNoBundle) {
		t.Errorf("no bundle: err = %v", err)
	}

	down := &Source{BaseURL: srv.URL + "/missing", HTTP: srv.Client(), Now: func() time.Time { return now }}
	if _, err := down.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "player page") {
		t.Errorf("page error = %v", err)
	}

	var hits atomic.Int32
	empty := player(t, "no token here", &hits)
	s = &Source{BaseURL: empty.URL, HTTP: empty.Client(), Now: func() time.Time { return now }}
	if _, err := s.Token(context.Background()); !errors.Is(err, ErrNoToken) {
		t.Errorf("no token: err = %v", err)
	}
}
