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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/trick77/ship/kit/env"
	"github.com/trick77/ship/kit/fsx"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/version"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loom-strava serve|healthcheck")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "healthcheck":
		os.Exit(httpx.Healthcheck(httpx.LocalURL(envOr("STRAVA_ADDR", defaultAddr), "/healthz")))
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loom-strava:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	if err := writable(cfg.data); err != nil {
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

// writable fails the start when dir cannot be written: a host dir Docker
// created for a bind mount is root-owned, and the sidecar would run healthy
// with no state and fail only on the first save.
func writable(dir string) error {
	p := filepath.Join(dir, ".write-check")
	if err := fsx.WriteFileAtomic(p, nil, 0o600); err != nil {
		return fmt.Errorf("data dir %s is not writable: on the host, chown 65532:65532 it: %w", dir, err)
	}
	return os.Remove(p)
}

// httpTimeout bounds one Strava request, under mcpserver's 25 s tool
// deadline: a hung call fails before the tool does.
const httpTimeout = 20 * time.Second

// shutdownTimeout lets a running tool call finish on SIGTERM; tools give up
// at 25 s.
const shutdownTimeout = 30 * time.Second

// envOr reads key through kit's env reader, as loadConfig does, so the
// healthcheck probes the address serve listens on.
func envOr(key, def string) string {
	return env.New(os.Getenv).String(key, def)
}
