// Command loom-imagegen serves loom's image tool over MCP (docs/imagegen.md):
// one tool, generate, rendered by fal. It holds the fal key and every model
// fact, so loom names no image vendor or model; any server answering the same
// contract replaces it.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/ship/kit/env"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/version"
)

// shutdownTimeout lets a running generation finish on SIGTERM: fal's render
// is paid for whether loom gets the picture or not. Inside compose's 3 min
// stop_grace_period, so the drain ends before Docker's SIGKILL.
const shutdownTimeout = 175 * time.Second

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loom-imagegen serve|healthcheck")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "healthcheck":
		// Read like serve reads it (trimmed, empty is unset), or the probe
		// would miss a server listening fine.
		os.Exit(httpx.Healthcheck(httpx.LocalURL(env.New(os.Getenv).String("IMAGEGEN_ADDR", defaultAddr), "/healthz")))
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loom-imagegen:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	srv := httpx.NewServer(cfg.addr, newServer(cfg, newService(cfg), version.Version))
	slog.Info("imagegen listening", "addr", cfg.addr, "model", cfg.model, "typography_model", cfg.typographyModel)
	return httpx.Serve(context.Background(), srv, shutdownTimeout)
}

// newService is the image service over fal as configured.
func newService(cfg config) *imagegen.Service {
	return imagegen.NewService(imagegen.NewFalClient(imagegen.FalConfig{
		BaseURL:     cfg.baseURL,
		APIKey:      cfg.apiKey,
		Model:       cfg.model,
		PollTimeout: cfg.pollTimeout,
	}), cfg.model, cfg.typographyModel)
}
