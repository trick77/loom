// Command loom-weather serves loom's weather over MCP (docs/weather.md): a
// forecast for a point and a place-name lookup, answered by Open-Meteo. It
// holds every fact about the provider, so loom names no weather vendor; any
// server answering the same contract replaces it.
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
	// providerTimeout bounds one provider request.
	providerTimeout = 10 * time.Second
	// shutdownTimeout lets a running request finish on SIGTERM.
	shutdownTimeout = 15 * time.Second
)

func main() {
	sidecar.Main("loom-weather", "WEATHER_ADDR", defaultAddr, serve)
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	srv := httpx.NewServer(cfg.addr, newServer(cfg, &http.Client{Timeout: providerTimeout}, version.Version))
	if cfg.token == "" {
		slog.Warn("WEATHER_TOKEN is unset: refusing every tool call, the weather stays off")
	}
	slog.Info("weather listening", "addr", cfg.addr)
	return httpx.Serve(context.Background(), srv, shutdownTimeout)
}
