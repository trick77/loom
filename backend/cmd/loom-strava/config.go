package main

import (
	"net/url"
	"strings"

	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr    = ":8050"
	defaultDataDir = "/data"
	defaultBaseURL = "https://www.strava.com"
	// minTokenLen matches loom's BACKEND_STRAVA_TOKEN floor.
	minTokenLen = 32
)

type config struct {
	addr         string
	token        string
	clientID     string
	clientSecret string
	data         string
	// base is Strava's origin: the API under /api/v3, the consent page under
	// /oauth. Tests point it at a fake.
	base *url.URL
}

// loadConfig reads STRAVA_* through kit's env reader: trimmed, empty is
// unset, like loom's side of the same token.
func loadConfig(getenv func(string) string) (config, error) {
	r := env.New(getenv)
	cfg := config{
		addr:         r.String("STRAVA_ADDR", defaultAddr),
		token:        r.String("STRAVA_TOKEN", ""),
		clientID:     r.Required("STRAVA_CLIENT_ID"),
		clientSecret: r.Required("STRAVA_CLIENT_SECRET"),
		data:         r.String("STRAVA_DATA", defaultDataDir),
		base:         r.URL("STRAVA_BASE_URL", defaultBaseURL),
	}
	// Required: this sidecar holds every user's Strava tokens, and nothing on
	// the network may read with them unasked.
	if len(cfg.token) < minTokenLen {
		r.Fail("STRAVA_TOKEN must be at least %d characters (openssl rand -hex 32)", minTokenLen)
	}
	if strings.ContainsFunc(cfg.token, func(c rune) bool { return c <= ' ' || c == 0x7f }) {
		r.Fail("STRAVA_TOKEN must not contain whitespace or control characters")
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
