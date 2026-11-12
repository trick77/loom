// Command loom-shop serves loom's product search over MCP (docs/shop.md): one
// tool, search_products, answered by Galaxus's own MCP server. It holds every
// fact about the shop, so loom names none; any server answering the same
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
	// upstreamTimeout bounds one request to the shop. The whole search (a
	// handshake, the call, a retry on a dropped session) shares the tool
	// call's context: mcpserver's 25 s, inside loom's 30 s per call.
	upstreamTimeout = 20 * time.Second
	// shutdownTimeout lets a running search finish on SIGTERM.
	shutdownTimeout = 25 * time.Second
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loom-shop serve|healthcheck")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "healthcheck":
		// Read like serve reads it (trimmed, empty is unset), or the probe
		// would miss a server listening fine.
		os.Exit(httpx.Healthcheck(httpx.LocalURL(env.New(os.Getenv).String("SHOP_ADDR", defaultAddr), "/healthz")))
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loom-shop:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	srv := httpx.NewServer(cfg.addr, newServer(cfg, &http.Client{Timeout: upstreamTimeout}, version.Version))
	if cfg.token == "" {
		slog.Warn("SHOP_TOKEN is unset: refusing every tool call, product search stays off")
	}
	if cfg.upstreamURL == "" {
		slog.Warn("SHOP_UPSTREAM_URL is off: serving no tool, product search stays off")
	}
	slog.Info("shop listening", "addr", cfg.addr, "product_search", cfg.upstreamURL != "")
	return httpx.Serve(context.Background(), srv, shutdownTimeout)
}
