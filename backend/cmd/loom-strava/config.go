package main

import (
	"net/url"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr    = ":8050"
	defaultDataDir = "/data"
	defaultBaseURL = "https://www.strava.com"
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
	// loom checks its side with the same rule.
	if msg := sidecar.TokenProblem("STRAVA_TOKEN", cfg.token); msg != "" {
		r.Fail("%s", msg)
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
