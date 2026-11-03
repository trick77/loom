// Command loom-tunewire bridges Apple Music to MCP for loom: the user's
// library playlists (/mcp/library) and catalog search (/mcp/catalog).
//
// Without a paid Apple developer account it reads the API the way the
// music.apple.com web player does: the player's own developer token, pulled
// from its JavaScript, plus the user's media-user-token cookie, pasted once in
// loom's settings and kept in /data. Unofficial: Apple can change either and
// break this. It only reads.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/trick77/loom/cmd/loom-tunewire/internal/applemusic"
	"github.com/trick77/loom/cmd/loom-tunewire/internal/webtoken"
	"github.com/trick77/ship/kit/env"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/version"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loom-tunewire serve|healthcheck")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "healthcheck":
		os.Exit(httpx.Healthcheck(httpx.LocalURL(envOr("TUNEWIRE_ADDR", defaultAddr), "/healthz")))
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loom-tunewire:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	dev := &webtoken.Source{HTTP: httpClient}
	client := &applemusic.Client{HTTP: httpClient, Dev: dev}
	svc, err := newService(client, fileStore{dir: cfg.data}, cfg.storefront)
	if err != nil {
		return err
	}
	dev.Seed(svc.devToken())
	dev.OnNew = svc.keepDevToken
	srv := httpx.NewServer(cfg.addr, newServer(cfg, svc, version.Version))
	slog.Info("tunewire listening", "addr", cfg.addr, "storefront", cfg.storefront, "accounts", svc.accounts())
	return httpx.Serve(context.Background(), srv, shutdownTimeout)
}

// shutdownTimeout lets a running tool call finish on SIGTERM; tools give up
// at 25s.
const shutdownTimeout = 30 * time.Second

// envOr reads key through kit's env reader, as loadConfig does, so the
// healthcheck probes the address serve listens on.
func envOr(key, def string) string {
	return env.New(os.Getenv).String(key, def)
}
