package main

import (
	"net/url"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr         = ":8020"
	defaultGotenbergURL = "http://gotenberg:3000"
)

type config struct {
	addr  string
	token string
	// gotenbergURL is the Gotenberg (Chromium) sidecar that renders PDFs.
	gotenbergURL string
}

// loadConfig reads DOCGEN_* through kit's env reader: trimmed, empty is unset.
func loadConfig(getenv func(string) string) (config, error) {
	r := env.New(getenv)
	cfg := config{
		addr:         r.String("DOCGEN_ADDR", defaultAddr),
		token:        r.String("DOCGEN_TOKEN", ""),
		gotenbergURL: r.String("DOCGEN_GOTENBERG_URL", defaultGotenbergURL),
	}
	// Required: whoever calls makes Gotenberg render and fills loom's
	// volumes with files. loom checks its side with the same rule.
	if msg := sidecar.TokenProblem("DOCGEN_TOKEN", cfg.token); msg != "" {
		r.Fail("%s", msg)
	}
	if u, err := url.Parse(cfg.gotenbergURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		r.Fail("DOCGEN_GOTENBERG_URL must be an absolute http(s) URL")
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
