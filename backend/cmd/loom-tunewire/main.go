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
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/trick77/loom/internal/sidecar"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/version"
	"github.com/trick77/toolkit/applemusic"
)

func main() {
	sidecar.Main("loom-tunewire", "TUNEWIRE_ADDR", defaultAddr, serve)
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	dev := &applemusic.Source{HTTP: httpClient}
	// A tool call waits on it: one try, and a 429 waits at most 5 s once.
	client := &applemusic.Client{HTTP: httpClient, Dev: dev, RateLimitWait: 5 * time.Second}
	if err := sidecar.Writable(cfg.data); err != nil {
		return err
	}
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
