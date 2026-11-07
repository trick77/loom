package main

import (
	"regexp"
	"strings"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr       = ":8060"
	defaultDataDir    = "/data"
	defaultStorefront = "ch"
)

var storefrontRe = regexp.MustCompile(`^[a-z]{2}$`)

type config struct {
	addr  string
	token string
	data  string
	// storefront is the catalog search's country: it decides which songs
	// exist and the language results come in.
	storefront string
}

// loadConfig reads TUNEWIRE_* through kit's env reader: trimmed, empty is
// unset, like loom's side of the same token.
func loadConfig(getenv func(string) string) (config, error) {
	r := env.New(getenv)
	cfg := config{
		addr:       r.String("TUNEWIRE_ADDR", defaultAddr),
		token:      r.String("TUNEWIRE_TOKEN", ""),
		data:       r.String("TUNEWIRE_DATA", defaultDataDir),
		storefront: strings.ToLower(r.String("TUNEWIRE_STOREFRONT", defaultStorefront)),
	}
	// Required, unlike the sandbox's: this sidecar holds a personal Apple
	// Music token, and nothing on the network may read with it unasked.
	// loom checks its side with the same rule.
	if msg := sidecar.TokenProblem("TUNEWIRE_TOKEN", cfg.token); msg != "" {
		r.Fail("%s", msg)
	}
	if !storefrontRe.MatchString(cfg.storefront) {
		r.Fail("TUNEWIRE_STOREFRONT must be a two-letter country code, got %q", cfg.storefront)
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
