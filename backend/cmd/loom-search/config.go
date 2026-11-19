package main

import (
	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/env"
)

const (
	defaultAddr      = ":8030"
	defaultTavilyURL = "https://api.tavily.com"
)

type config struct {
	addr  string
	token string
	// tavilyKey empty serves no tool: web search stays off, the sidecar up.
	tavilyKey string
	tavilyURL string
}

// loadConfig reads SEARCH_* through kit's env reader: trimmed, empty is unset.
func loadConfig(getenv func(string) string) (config, error) {
	r := env.New(getenv)
	cfg := config{
		addr:      r.String("SEARCH_ADDR", defaultAddr),
		token:     r.String("SEARCH_TOKEN", ""),
		tavilyKey: r.String("SEARCH_TAVILY_API_KEY", ""),
	}
	// Required: whoever calls spends the Tavily account's credits. loom
	// checks its side with the same rule.
	if msg := sidecar.TokenProblem("SEARCH_TOKEN", cfg.token); msg != "" {
		r.Fail("%s", msg)
	}
	if u := r.URL("SEARCH_TAVILY_URL", defaultTavilyURL); u != nil {
		cfg.tavilyURL = u.String()
	}
	if err := r.Err(); err != nil {
		return config{}, err
	}
	return cfg, nil
}
