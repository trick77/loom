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
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/config"
	"github.com/trick77/spark/internal/httpapi"
	"github.com/trick77/spark/internal/llm"
	"github.com/trick77/spark/internal/mcp"
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
	if _, err := sessionStore.DeleteExpired(context.Background()); err != nil {
		return err
	}
	authMW := auth.NewMiddleware(sessionStore, userStore)
	chatStore := chat.NewStore(db)
	var chatClient httpapi.ChatClient
	if cfg.ChatBaseURL != "" {
		chatClient = llm.NewClient(chatClientConfigFromConfig(cfg), http.DefaultClient)
	}
	var toolService httpapi.ToolService
	mcpConfig := mcp.Config{}
	if cfg.MCPConfigPath != "" {
		loadedMCPConfig, err := mcp.LoadConfig(cfg.MCPConfigPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
			slog.Warn("MCP config not found; external MCP tools disabled", "path", cfg.MCPConfigPath)
		} else {
			mcpConfig = loadedMCPConfig
		}
	}
	var searxngNameCollision bool
	mcpConfig, searxngNameCollision = toolConfigForConfig(cfg, mcpConfig)
	if searxngNameCollision {
		slog.Warn("built-in SearXNG tool disabled because MCP config already defines server name searxng")
	}
	if len(mcpConfig.Servers) > 0 {
		discoveryCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		discovered, err := mcp.NewBestEffortServiceFromConfig(discoveryCtx, mcpConfig, &http.Client{Timeout: 15 * time.Second}, slog.Default())
		cancel()
		if err != nil {
			return err
		}
		toolService = discovered
		slog.Info("tools discovered", "count", len(discovered.Tools()))
	}
	var oidcService httpapi.OIDCService
	var devAuthClaims auth.Claims
	if cfg.AuthMode == config.AuthModeOIDC {
		discoveredOIDC, err := auth.NewOIDCServiceFromDiscovery(context.Background(), auth.OIDCServiceConfig{
			Issuer:       cfg.OIDC.Issuer,
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			RedirectURL:  cfg.OIDC.RedirectURL,
			SecureCookie: secureCookies,
		})
		if err != nil {
			return err
		}
		oidcService = discoveredOIDC
	}
	if cfg.AuthMode == config.AuthModeDev {
		slog.Warn("development auth enabled; local loopback use only")
		devAuthClaims = auth.Claims{
			Subject:  cfg.DevUser.Subject,
			Username: cfg.DevUser.Username,
			Email:    cfg.DevUser.Email,
			Name:     cfg.DevUser.DisplayName,
			Groups:   []string{auth.DevAdminGroup},
		}
	}

	handler := httpapi.New(httpapi.Deps{
		Version:               version,
		Static:                web.SPAHandler(),
		OIDC:                  oidcService,
		Auth:                  authMW,
		Sessions:              sessionStore,
		Users:                 userStore,
		Chat:                  chatStore,
		LLM:                   chatClient,
		MCP:                   toolService,
		OIDCAdminGroup:        cfg.OIDC.AdminGroup,
		DevAuthClaims:         devAuthClaims,
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

func responseLogDirForConfig(cfg config.Config) string {
	if cfg.AuthMode != config.AuthModeDev {
		return ""
	}
	return cfg.ChatLogDir
}

func chatClientConfigFromConfig(cfg config.Config) llm.Config {
	return llm.Config{
		BaseURL:         cfg.ChatBaseURL,
		APIKey:          cfg.ChatAPIKey,
		Model:           cfg.ChatModel,
		ReasoningEffort: cfg.ChatReasoningEffort,
		ResponseLogDir:  responseLogDirForConfig(cfg),
	}
}

func toolConfigForConfig(cfg config.Config, base mcp.Config) (mcp.Config, bool) {
	out := mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	for name, server := range base.Servers {
		out.Servers[name] = server
	}
	if strings.TrimSpace(cfg.SearxngURL) != "" {
		if _, exists := out.Servers["searxng"]; exists {
			return out, true
		}
		out.Servers["searxng"] = mcp.SearxngServerConfig(cfg.SearxngURL)
	}
	return out, false
}
