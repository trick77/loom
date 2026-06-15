// Command slopr is the all-in-one server: API + embedded SPA, backed by SQLite.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/trick77/slopr/internal/artifact"
	"github.com/trick77/slopr/internal/auth"
	"github.com/trick77/slopr/internal/chat"
	"github.com/trick77/slopr/internal/config"
	"github.com/trick77/slopr/internal/docgen"
	"github.com/trick77/slopr/internal/documents"
	"github.com/trick77/slopr/internal/httpapi"
	"github.com/trick77/slopr/internal/imagegen"
	"github.com/trick77/slopr/internal/llm"
	"github.com/trick77/slopr/internal/mcp"
	"github.com/trick77/slopr/internal/rag"
	"github.com/trick77/slopr/internal/store"
	"github.com/trick77/slopr/internal/usage"
	"github.com/trick77/slopr/web"
)

var version = "dev" // overridden via -ldflags at build time

func main() {
	// Configure structured logging with an explicit handler so every line
	// carries an RFC3339 timestamp (the package default does not guarantee one).
	// The level is tunable via BACKEND_LOG_LEVEL (debug/info/warn/error).
	logLevel := parseLogLevel(envDefault("BACKEND_LOG_LEVEL", "info"))
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(healthcheckURL(envDefault("BACKEND_ADDR", ":8080"))); err != nil {
			slog.Error("healthcheck failed", "err", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func envDefault(key, def string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return def
}

// parseLogLevel maps a BACKEND_LOG_LEVEL string to a slog.Level, defaulting to
// Info for empty or unrecognized values.
func parseLogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func healthcheckURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:8080/api/health"
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/api/health"
}

func runHealthcheck(url string) error {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected health status %d", resp.StatusCode)
	}
	return nil
}

func run() error {
	// Logged first thing so the running build is identifiable even if startup later
	// fails (config error, DB open, etc.) — the "listening" line only lands on success.
	slog.Info("starting slopr", "version", version)

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
	usageStore := usage.NewStore(db)

	// Built before the RAG block so the ingester can use it to describe image
	// documents; reused below as the chat client.
	var llmClient *llm.Client
	if cfg.ChatBaseURL != "" {
		llmClient = llm.NewClient(chatClientConfigFromConfig(cfg), http.DefaultClient)
	}

	// Document RAG is enabled only when an embeddings endpoint is configured.
	var documentService httpapi.DocumentService
	if strings.TrimSpace(cfg.EmbedBaseURL) != "" {
		ragStore := rag.NewStore(db)
		if err := ragStore.ResetStuckIngestions(context.Background()); err != nil {
			return err
		}
		// One-time data fix: rebind or remove pre-thread-scoping uploads that were
		// stored user-global and leaked into unrelated chats.
		if err := ragStore.ReconcileLegacyDocumentScopes(context.Background()); err != nil {
			return err
		}
		// One-time data fix: remove stale citation metadata from old assistant
		// messages after legacy document scopes have been corrected.
		if err := ragStore.ScrubOutOfScopeMessageCitations(context.Background()); err != nil {
			return err
		}
		embedClient := rag.NewEmbedClient(rag.EmbedConfig{
			BaseURL: cfg.EmbedBaseURL,
			APIKey:  cfg.EmbedAPIKey,
			Model:   cfg.EmbedModel,
		}, http.DefaultClient)
		tikaClient := documents.NewTikaClient(documents.TikaConfig{BaseURL: cfg.TikaURL})
		ingester := rag.NewIngester(ragStore, documents.VolumeOpener{UsersDir: cfg.UsersDir}, tikaClient, embedClient)
		ingester.SetUsageRecorder(usageStore)
		if llmClient != nil {
			ingester.SetImageDescriber(llmClient)
		}
		docs := documents.NewService(ragStore, artifactStore, ingester, embedClient, cfg.UsersDir)
		docs.SetUsageRecorder(usageStore)
		documentService = docs
	}
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
			BaseURL:     cfg.BFLBaseURL,
			APIKey:      cfg.BFLAPIKey,
			Model:       cfg.BFLModel,
			PollTimeout: cfg.BFLPollTimeout,
		})
		imageTools = append(imageTools, imagegen.NewTool(imageProvider))
	}
	var chatClient httpapi.ChatClient
	if llmClient != nil {
		chatClient = llmClient
	}
	var toolService httpapi.ToolService
	mcpConfig := toolConfigForConfig(cfg)
	if len(mcpConfig.Servers) > 0 {
		// Give discovery room for cold MCP sidecars: the fetch bridge boots a
		// Python interpreter on its first tools/list, and browser sidecars can be
		// slow on cold starts. The per-request and overall budgets are aligned so
		// a single slow server can actually use its window.
		discoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		discovered, err := mcp.NewRequiredServiceFromConfig(discoveryCtx, mcpConfig, &http.Client{Timeout: 30 * time.Second})
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
		Version:                    version,
		Static:                     web.SPAHandler(),
		OIDC:                       oidcService,
		Auth:                       authMW,
		Sessions:                   sessionStore,
		Users:                      userStore,
		Chat:                       chatStore,
		Usage:                      usageStore,
		Artifacts:                  artifactStore,
		Documents:                  documentService,
		LLM:                        chatClient,
		MCP:                        toolService,
		DocTools:                   docTools,
		ImageTools:                 imageTools,
		UsersDir:                   cfg.UsersDir,
		OIDCAdminGroup:             cfg.OIDC.AdminGroup,
		DevAuthClaims:              devAuthClaims,
		PostLogoutRedirectURL:      cfg.OIDC.PostLogoutRedirectURL,
		KnowledgeInlineTokenBudget: cfg.KnowledgeInlineTokenBudget,
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
		BaseURL:             cfg.ChatBaseURL,
		APIKey:              cfg.ChatAPIKey,
		MaxCompletionTokens: cfg.ChatMaxCompletionTokens,
		Timeout:             cfg.ChatTimeout,
		IdleTimeout:         cfg.ChatIdleTimeout,
		ResponseLogDir:      responseLogDirForConfig(cfg),
	}
}

func toolConfigForConfig(cfg config.Config) mcp.Config {
	out := mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	if strings.TrimSpace(cfg.FetchMCPURL) != "" {
		out.Servers["fetch"] = mcp.FetchServerConfig(cfg.FetchMCPURL)
	}
	if strings.TrimSpace(cfg.ObscuraMCPURL) != "" {
		out.Servers["obscura"] = mcp.ObscuraServerConfig(cfg.ObscuraMCPURL)
	}
	if strings.TrimSpace(cfg.TavilyAPIKey) != "" {
		out.Servers["tavily"] = mcp.TavilyServerConfig(cfg.TavilyURL, cfg.TavilyAPIKey)
	}
	if strings.TrimSpace(cfg.Context7APIKey) != "" {
		out.Servers["context7"] = mcp.Context7ServerConfig(cfg.Context7MCPURL, cfg.Context7APIKey)
	}
	return out
}

func context7Configured(cfg config.Config) bool {
	return strings.TrimSpace(cfg.Context7APIKey) != ""
}

func tavilyConfigured(cfg config.Config) bool {
	return strings.TrimSpace(cfg.TavilyAPIKey) != ""
}

func bflImageConfigured(cfg config.Config) bool {
	return strings.TrimSpace(cfg.BFLAPIKey) != ""
}
