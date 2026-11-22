package main

import (
	"log/slog"
	"strings"
	"time"

	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr    = ":8040"
	defaultBaseURL = "https://queue.fal.run"
	// The models fal renders with: the default for ordinary pictures, the
	// typography one for text-heavy work (infographics, mind maps, posters).
	// FLUX.2 [max] there garbled dense German text into glyph soup; GPT Image 2
	// at medium renders it and costs less ($0.040 at 1920×1080 vs $0.07 at
	// 1024×576).
	defaultModel           = "fal-ai/flux-2-pro"
	defaultTypographyModel = "openai/gpt-image-2"
	// defaultPollTimeout waits as long as allowed: GPT Image took over a
	// minute for a dense ~2 MP mind map and timed out at the old 1 min.
	defaultPollTimeout = maxPollTimeout
	maxPollTimeout     = imagegen.MaxPollTimeout
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
		model:           r.String("IMAGEGEN_MODEL", defaultModel),
		typographyModel: r.String("IMAGEGEN_TYPOGRAPHY_MODEL", defaultTypographyModel),
		pollTimeout:     r.Duration("IMAGEGEN_POLL_TIMEOUT", defaultPollTimeout),
	}
	if strings.EqualFold(cfg.typographyModel, "off") {
		cfg.typographyModel = ""
	}
	// Required: whoever calls spends the fal account's money. loom checks its
	// side with the same rule.
	if msg := sidecar.TokenProblem("IMAGEGEN_TOKEN", cfg.token); msg != "" {
		r.Fail("%s", msg)
	}
	if cfg.apiKey == "" {
		r.Fail("IMAGEGEN_API_KEY is required (fal API key)")
	}
	if u := r.URL("IMAGEGEN_BASE_URL", defaultBaseURL); u != nil {
		cfg.baseURL = u.String()
	}
	if cfg.pollTimeout <= 0 {
		r.Fail("IMAGEGEN_POLL_TIMEOUT must be greater than 0")
	}
	// Longer is cut, not refused: 2m30s was the documented maximum, and an
	// install that set it must still boot.
	if cfg.pollTimeout > maxPollTimeout {
		slog.Warn("IMAGEGEN_POLL_TIMEOUT past the call limit, using the most allowed", "set", cfg.pollTimeout, "used", maxPollTimeout)
		cfg.pollTimeout = maxPollTimeout
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
