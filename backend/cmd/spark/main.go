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

	"github.com/trick77/spark/internal/artifact"
	"github.com/trick77/spark/internal/auth"
	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/config"
	"github.com/trick77/spark/internal/docgen"
	"github.com/trick77/spark/internal/httpapi"
	"github.com/trick77/spark/internal/imagegen"
	"github.com/trick77/spark/internal/llm"
	"github.com/trick77/spark/internal/mcp"
	"github.com/trick77/spark/internal/store"
	"github.com/trick77/spark/web"
)

var version = "dev" // overridden via -ldflags at build time

func main() {
	// Configure structured logging with an explicit handler so every line
	// carries an RFC3339 timestamp (the package default does not guarantee one).
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
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
	artifactStore := artifact.NewStore(db)
	docTools := []docgen.Generator{
		docgen.TextGenerator{},
		docgen.PDFGenerator{},
		docgen.XLSXGenerator{},
		docgen.DOCXGenerator{},
		docgen.PPTXGenerator{},
	}
	var imageTools []imagegen.Tool
	if bflImageConfigured(cfg) {
		imageProvider := imagegen.NewBFLClient(imagegen.BFLConfig{
			BaseURL: cfg.BFLBaseURL,
			APIKey:  cfg.BFLAPIKey,
			Model:   cfg.BFLModel,
		})
		imageTools = append(imageTools, imagegen.NewTool(imageProvider))
	}
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
	var builtInToolNameCollision bool
	mcpConfig, builtInToolNameCollision = toolConfigForConfig(cfg, mcpConfig)
	if builtInToolNameCollision {
		slog.Warn("built-in MCP tool disabled because MCP config already defines the same server name")
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
	discoveredTools := 0
	if toolService != nil {
		discoveredTools = len(toolService.Tools())
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
	logStartupCapabilities(cfg, mcpConfig, startupRuntime{
		DocToolCount:        len(docTools),
		ImageToolCount:      len(imageTools),
		DiscoveredToolCount: discoveredTools,
	})

	handler := httpapi.New(httpapi.Deps{
		Version:               version,
		Static:                web.SPAHandler(),
		OIDC:                  oidcService,
		Auth:                  authMW,
		Sessions:              sessionStore,
		Users:                 userStore,
		Chat:                  chatStore,
		Artifacts:             artifactStore,
		LLM:                   chatClient,
		MCP:                   toolService,
		DocTools:              docTools,
		ImageTools:            imageTools,
		UsersDir:              cfg.UsersDir,
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
	if strings.TrimSpace(cfg.TavilyAPIKey) != "" {
		if _, exists := out.Servers["tavily"]; exists {
			return out, true
		}
		out.Servers["tavily"] = mcp.TavilyServerConfig(cfg.TavilyURL, cfg.TavilyAPIKey)
	}
	if strings.TrimSpace(cfg.Context7APIKey) != "" {
		if _, exists := out.Servers["context7"]; exists {
			return out, true
		}
		out.Servers["context7"] = mcp.Context7ServerConfig(cfg.Context7MCPURL, cfg.Context7APIKey)
	}
	return out, false
}

func context7Configured(cfg config.Config, base mcp.Config) bool {
	if strings.TrimSpace(cfg.Context7APIKey) != "" {
		return true
	}
	_, exists := base.Servers["context7"]
	return exists
}

func tavilyConfigured(cfg config.Config, base mcp.Config) bool {
	if strings.TrimSpace(cfg.TavilyAPIKey) != "" {
		return true
	}
	_, exists := base.Servers["tavily"]
	return exists
}

func bflImageConfigured(cfg config.Config) bool {
	return strings.TrimSpace(cfg.BFLAPIKey) != ""
}
