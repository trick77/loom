// Command loom-docgen serves loom's document tools over MCP (docs/docgen.md):
// text, PDF (rendered by Gotenberg), XLSX, DOCX and PPTX files, each returned
// as a binary resource loom keeps as an artifact. Any server answering the
// same contract replaces it.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/ship/kit/env"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/version"
)

const (
	// gotenbergReadyTimeout bounds the boot probe of Gotenberg. compose
	// starts it healthy first; this only absorbs a restart race.
	gotenbergReadyTimeout = 15 * time.Second
	// shutdownTimeout lets a running render finish on SIGTERM, inside
	// compose's default 10 s stop grace. A render cut short costs nothing:
	// loom reports the call failed and the model may ask again.
	shutdownTimeout = 8 * time.Second
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loom-docgen serve|healthcheck")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "healthcheck":
		// Read like serve reads it (trimmed, empty is unset), or the probe
		// would miss a server listening fine.
		os.Exit(httpx.Healthcheck(httpx.LocalURL(env.New(os.Getenv).String("DOCGEN_ADDR", defaultAddr), "/healthz")))
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loom-docgen:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	gotenberg := docgen.NewGotenbergClient(docgen.GotenbergConfig{BaseURL: cfg.gotenbergURL})
	if err := requireGotenberg(gotenberg.Ping, cfg.gotenbergURL); err != nil {
		return err
	}
	srv := httpx.NewServer(cfg.addr, newServer(cfg, generators(gotenberg), version.Version))
	slog.Info("docgen listening", "addr", cfg.addr, "gotenberg", cfg.gotenbergURL)
	return httpx.Serve(context.Background(), srv, shutdownTimeout)
}

// requireGotenberg fails boot while Gotenberg does not answer: up, the
// sidecar would offer create_pdf_file calls that can only fail. compose
// restarts it until Gotenberg is there.
func requireGotenberg(ping func(context.Context) error, url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), gotenbergReadyTimeout)
	defer cancel()
	if err := ping(ctx); err != nil {
		return fmt.Errorf("gotenberg not reachable at %s: %w", url, err)
	}
	return nil
}
