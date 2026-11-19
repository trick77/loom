// Command loom-shop serves loom's product search over MCP (docs/shop.md): one
// tool, search_products, answered by Galaxus's own MCP server. It holds every
// fact about the shop, so loom names none; any server answering the same
// contract replaces it.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/trick77/loom/internal/sidecar"
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
	sidecar.Main("loom-shop", "SHOP_ADDR", defaultAddr, serve)
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
