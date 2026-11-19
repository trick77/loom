// Command loom-strava serves each loom user's own Strava data to loom as MCP
// tools, read through Strava's official OAuth API.
//
// Every loom user connects their own Strava account; the sidecar keeps one
// connection per loom user id (X-Loom-User) and never lets one user's call
// read, use or delete another's token, cache or metrics. The client secret and
// every Strava token stay here; loom holds only the service bearer.
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

func main() {
	sidecar.Main("loom-strava", "STRAVA_ADDR", defaultAddr, serve)
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	if err := sidecar.Writable(cfg.data); err != nil {
		return err
	}
	svc, err := newService(cfg, &http.Client{Timeout: httpTimeout}, fileStore{dir: cfg.data})
	if err != nil {
		return err
	}
	srv := httpx.NewServer(cfg.addr, newServer(cfg, svc, version.Version))
	slog.Info("strava listening", "addr", cfg.addr, "connections", svc.connections())
	return httpx.Serve(context.Background(), srv, shutdownTimeout)
}

// httpTimeout bounds one Strava request, under mcpserver's 25 s tool
// deadline: a hung call fails before the tool does.
const httpTimeout = 20 * time.Second

// shutdownTimeout lets a running tool call finish on SIGTERM; tools give up
// at 25 s.
const shutdownTimeout = 30 * time.Second
