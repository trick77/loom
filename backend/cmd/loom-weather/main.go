// Command loom-weather serves loom's weather over MCP (docs/weather.md): a
// forecast for a point and a place-name lookup, answered by Open-Meteo. It
// holds every fact about the provider, so loom names no weather vendor; any
// server answering the same contract replaces it.
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
	// providerTimeout bounds one provider request.
	providerTimeout = 10 * time.Second
	// shutdownTimeout lets a running request finish on SIGTERM.
	shutdownTimeout = 15 * time.Second
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loom-weather serve|healthcheck")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "healthcheck":
		os.Exit(httpx.Healthcheck(httpx.LocalURL(env.New(os.Getenv).String("WEATHER_ADDR", defaultAddr), "/healthz")))
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loom-weather:", err)
		os.Exit(1)
	}
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
