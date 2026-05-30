// Command spark is the all-in-one server: API + embedded SPA, backed by SQLite.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/config"
	"github.com/trick77/spark/internal/httpapi"
	"github.com/trick77/spark/internal/store"
	"github.com/trick77/spark/web"
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

	secureCookies := strings.HasPrefix(cfg.PublicURL, "https://")
	userStore := auth.NewUserStore(db)
	sessionStore := auth.NewSessionStore(db, secureCookies)
	authMW := auth.NewMiddleware(sessionStore, userStore)
	var oidcService *auth.OIDCService
	if cfg.OIDC.Issuer != "" {
		oidcService, err = auth.NewOIDCServiceFromDiscovery(context.Background(), auth.OIDCServiceConfig{
			Issuer:       cfg.OIDC.Issuer,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.OIDC.RedirectURL,
			SecureCookie: secureCookies,
		})
		if err != nil {
			return err
		}
	}

	handler := httpapi.New(httpapi.Deps{
		Version:               version,
		Static:                web.SPAHandler(),
		OIDC:                  oidcService,
		Auth:                  authMW,
		Sessions:              sessionStore,
		Users:                 userStore,
		OIDCAdminGroup:        cfg.OIDC.AdminGroup,
		PostLogoutRedirectURL: cfg.OIDC.PostLogoutRedirectURL,
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
