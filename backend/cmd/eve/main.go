// Command eve is the all-in-one server: API + embedded SPA, backed by SQLite.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/trick77/eve/internal/config"
	"github.com/trick77/eve/internal/httpapi"
	"github.com/trick77/eve/internal/store"
	"github.com/trick77/eve/web"
)

var version = "dev" // overridden via -ldflags at build time

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	handler := httpapi.New(httpapi.Deps{
		Version: version,
		Static:  web.SPAHandler(),
	})

	srv := &http.Server{Addr: cfg.Addr, Handler: handler}

	go func() {
		slog.Info("listening", "addr", cfg.Addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
