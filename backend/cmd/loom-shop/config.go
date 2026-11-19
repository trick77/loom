package main

import (
	"strings"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr        = ":8110"
	defaultUpstreamURL = "https://mcp.galaxus.ch/"
)

type config struct {
	addr  string
	token string
	// upstreamURL is the shop's MCP endpoint; "" (off) serves no tool.
	upstreamURL string
}

// loadConfig reads SHOP_* through kit's env reader: trimmed, empty is unset.
func loadConfig(getenv func(string) string) (config, error) {
	r := env.New(getenv)
	cfg := config{
		addr:  r.String("SHOP_ADDR", defaultAddr),
		token: r.String("SHOP_TOKEN", ""),
	}
	// Like the weather: compose starts the sidecar on every install, so a
	// missing token keeps it up (health probe) and refuses every call; a token
	// that is set must be a good one. loom checks its side alike.
	if msg := sidecar.TokenProblem("SHOP_TOKEN", cfg.token); cfg.token != "" && msg != "" {
		r.Fail("%s", msg)
	}
	if !strings.EqualFold(r.String("SHOP_UPSTREAM_URL", ""), "off") {
		if u := r.URL("SHOP_UPSTREAM_URL", defaultUpstreamURL); u != nil {
			cfg.upstreamURL = u.String()
		}
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
