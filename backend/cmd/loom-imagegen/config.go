package main

import (
	"net/url"
	"strings"
	"time"

	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr    = ":8040"
	defaultBaseURL = "https://queue.fal.run"
	// The models fal renders with: the default for ordinary pictures, the
	// typography one (renders legible text far better) for text-heavy work.
	defaultModel           = "fal-ai/flux-2-pro"
	defaultTypographyModel = "fal-ai/flux-2-max"
	defaultPollTimeout     = time.Minute
	// maxPollTimeout keeps fal's polling inside the tool's 170 s and loom's
	// 3 min call limit, with room to fetch and send the picture.
	maxPollTimeout = 2*time.Minute + 30*time.Second
	// minTokenLen matches loom's BACKEND_IMAGEGEN_TOKEN floor.
	minTokenLen = 32
)

type config struct {
	addr  string
	token string
	// apiKey, baseURL: fal's queue API.
	apiKey  string
	baseURL string
	model   string
	// typographyModel is "" when IMAGEGEN_TYPOGRAPHY_MODEL=off: every image
	// stays on model.
	typographyModel string
	pollTimeout     time.Duration
}

// loadConfig reads IMAGEGEN_* through kit's env reader: trimmed, empty is
// unset.
func loadConfig(getenv func(string) string) (config, error) {
	r := env.New(getenv)
	cfg := config{
		addr:            r.String("IMAGEGEN_ADDR", defaultAddr),
		token:           r.String("IMAGEGEN_TOKEN", ""),
		apiKey:          r.String("IMAGEGEN_API_KEY", ""),
		baseURL:         r.String("IMAGEGEN_BASE_URL", defaultBaseURL),
		model:           r.String("IMAGEGEN_MODEL", defaultModel),
		typographyModel: r.String("IMAGEGEN_TYPOGRAPHY_MODEL", defaultTypographyModel),
		pollTimeout:     r.Duration("IMAGEGEN_POLL_TIMEOUT", defaultPollTimeout),
	}
	if strings.EqualFold(cfg.typographyModel, "off") {
		cfg.typographyModel = ""
	}
	// Required: whoever calls spends the fal account's money.
	if len(cfg.token) < minTokenLen {
		r.Fail("IMAGEGEN_TOKEN must be at least %d characters (openssl rand -hex 32)", minTokenLen)
	}
	if strings.ContainsFunc(cfg.token, func(c rune) bool { return c <= ' ' || c == 0x7f }) {
		r.Fail("IMAGEGEN_TOKEN must not contain whitespace or control characters")
	}
	if cfg.apiKey == "" {
		r.Fail("IMAGEGEN_API_KEY is required (fal API key)")
	}
	if u, err := url.Parse(cfg.baseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		r.Fail("IMAGEGEN_BASE_URL must be an absolute http(s) URL")
	}
	if cfg.pollTimeout <= 0 || cfg.pollTimeout > maxPollTimeout {
		r.Fail("IMAGEGEN_POLL_TIMEOUT must be greater than 0 and at most %s (the call limit)", maxPollTimeout)
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
