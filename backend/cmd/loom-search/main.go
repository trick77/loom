// Command loom-search serves loom's web search over MCP (docs/search.md): one
// tool, web, answered by Tavily. It holds the Tavily key and every fact about
// the provider, so loom names no search vendor; any server answering the same
// contract replaces it.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/trick77/ship/kit/env"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/version"
)

const (
	// providerTimeout bounds one provider request, inside the tool's 25 s.
	providerTimeout = 20 * time.Second
	// shutdownTimeout lets a running search finish on SIGTERM.
	shutdownTimeout = 25 * time.Second
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loom-search serve|healthcheck")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "healthcheck":
		// Read like serve reads it (trimmed, empty is unset), or the probe
		// would miss a server listening fine.
		os.Exit(httpx.Healthcheck(httpx.LocalURL(env.New(os.Getenv).String("SEARCH_ADDR", defaultAddr), "/healthz")))
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loom-search:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	srv := httpx.NewServer(cfg.addr, newServer(cfg, &http.Client{Timeout: providerTimeout}, version.Version))
	if cfg.tavilyKey == "" {
		slog.Warn("SEARCH_TAVILY_API_KEY is unset: serving no tool, web search stays off")
	}
	slog.Info("search listening", "addr", cfg.addr, "web_search", cfg.tavilyKey != "")
	return httpx.Serve(context.Background(), srv, shutdownTimeout)
}
