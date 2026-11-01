package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	defaultAddr       = ":8060"
	defaultDataDir    = "/data"
	defaultStorefront = "ch"
	// minTokenLen matches loom's BACKEND_TUNEWIRE_TOKEN floor.
	minTokenLen = 32
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

func loadConfig(getenv func(string) string) (config, error) {
	or := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	cfg := config{
		addr:       or("TUNEWIRE_ADDR", defaultAddr),
		token:      getenv("TUNEWIRE_TOKEN"),
		data:       or("TUNEWIRE_DATA", defaultDataDir),
		storefront: strings.ToLower(or("TUNEWIRE_STOREFRONT", defaultStorefront)),
	}
	// Required, unlike the sandbox's: this sidecar holds a personal Apple
	// Music token, and nothing on the network may read with it unasked.
	if len(cfg.token) < minTokenLen {
		return config{}, fmt.Errorf("TUNEWIRE_TOKEN must be at least %d characters (openssl rand -hex 32)", minTokenLen)
	}
	if strings.ContainsFunc(cfg.token, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return config{}, errors.New("TUNEWIRE_TOKEN must not contain whitespace or control characters")
	}
	if !storefrontRe.MatchString(cfg.storefront) {
		return config{}, fmt.Errorf("TUNEWIRE_STOREFRONT must be a two-letter country code, got %q", cfg.storefront)
	}
	return cfg, nil
}
