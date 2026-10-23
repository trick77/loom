// Command loom is the all-in-one server: API + embedded SPA, backed by SQLite.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/trick77/ship/kit/env"
	"github.com/trick77/ship/kit/httpx"
	"github.com/trick77/ship/kit/logging"
	"github.com/trick77/ship/kit/run"
	"github.com/trick77/ship/kit/version"

	"github.com/trick77/loom/internal/artifact"
	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/background"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/config"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/documents"
	"github.com/trick77/loom/internal/httpapi"
	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/loom/internal/sandbox"
	"github.com/trick77/loom/internal/store"
	"github.com/trick77/loom/internal/usage"
	"github.com/trick77/loom/web"
)

func main() {
	// The container probe (compose healthcheck) runs before any setup: it must
	// answer even when the rest of the environment is broken.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(httpx.Healthcheck(httpx.LocalURL(env.New(os.Getenv).String("BACKEND_ADDR", ":8080"), "/healthz")))
	}
	// kit's text handler: RFC3339 timestamps, and the request id on every line
	// a handler logs with its request's context. A bad BACKEND_LOG_LEVEL fails
	// boot instead of logging at info.
	if _, err := logging.Setup(logging.Options{Level: os.Getenv("BACKEND_LOG_LEVEL"), LevelVar: "BACKEND_LOG_LEVEL"}); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
	if err := runServer(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func runServer() error {
	// Logged first thing so the running build is identifiable even if startup later
	// fails (config error, DB open, etc.) — the "listening" line only lands on success.
	slog.Info("starting loom", "version", version.String())

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	secureCookies := strings.HasPrefix(cfg.PublicURL, "https://")
	userStore := auth.NewUserStore(db)
	sessionStore := auth.NewSessionStore(db, secureCookies)
	if _, err := sessionStore.DeleteExpired(context.Background()); err != nil {
		return err
	}
	authMW := auth.NewMiddleware(sessionStore, userStore)
	threadStore := chat.NewStore(db)
	artifactStore := artifact.NewStore(db)
	usageStore := usage.NewStore(db)

	// Built before the RAG block so the ingester can use it to describe image
	// documents; reused below as the chat client.
	var llmClient *llm.Client
	if cfg.ChatEnabled {
		llmClient, err = llm.NewClient(chatClientConfigFromConfig(cfg), http.DefaultClient)
		if err != nil {
			return err
		}
	}

	// Document RAG is enabled only when an embeddings key is configured.
	var documentService httpapi.DocumentService
	var fetchPDF mcp.PDFExtractor
	reembed := func(context.Context) {}
	if cfg.EmbedEnabled {
		ragStore := rag.NewStore(db)
		if err := ragStore.ResetStuckIngestions(context.Background()); err != nil {
			return err
		}
		// One-time data fix: rebind or remove pre-thread-scoping uploads that were
		// stored user-global and leaked into unrelated threads.
		if err := ragStore.ReconcileLegacyDocumentScopes(context.Background()); err != nil {
			return err
		}
		// One-time data fix: remove stale citation metadata from old assistant
		// messages after legacy document scopes have been corrected.
		if err := ragStore.ScrubOutOfScopeMessageCitations(context.Background()); err != nil {
			return err
		}
		embedClient, err := rag.NewEmbedClient(rag.EmbedConfig{Model: cfg.EmbedModel.ID}, http.DefaultClient)
		if err != nil {
			return err
		}
		// A new embedding model with another width: rebuild the vector table at
		// the model's width. The chunks stay; the re-embed worker restores their
		// vectors below.
		if err := reconcileVectorWidth(context.Background(), ragStore, cfg.EmbedModel); err != nil {
			return err
		}
		// vec0 never frees a deleted vector's storage, and every search reads
		// all of it (rag.CompactVectors). Compacted here, before the listener
		// opens: the rebuild holds the write lock, which at boot blocks nobody.
		ragStore.CompactVectorsAtBoot(context.Background())
		tikaClient := documents.NewTikaClient(documents.TikaConfig{BaseURL: cfg.TikaURL})
		// Tika is an essential dependency of document RAG: fail fast at boot rather
		// than surface opaque extraction errors on the first upload.
		if err := requireSidecar("tika", cfg.TikaURL, tikaClient.Ping); err != nil {
			return err
		}
		fetchPDF = tikaClient.ExtractPDF
		ingester := rag.NewIngester(ragStore, documents.VolumeOpener{UsersDir: cfg.UsersDir}, tikaClient, embedClient)
		ingester.SetUsageRecorder(usageStore)
		if llmClient != nil {
			ingester.SetImageDescriber(llmClient)
		}
		reembed = func(ctx context.Context) { reembedUntilDone(ctx, ingester.ReembedMissing, run.Sleep) }
		docs := documents.NewService(ragStore, artifactStore, ingester, embedClient, cfg.UsersDir)
		docs.SetUsageRecorder(usageStore)
		documentService = docs
	}
	gotenbergClient := docgen.NewGotenbergClient(docgen.GotenbergConfig{BaseURL: cfg.GotenbergURL})
	// Gotenberg backs PDF export, and the doc tools are always offered (config
	// requires a users dir). Fail fast at boot rather than let the model start a
	// create_pdf_file call that can only end in a per-request "tool failed".
	if err := requireSidecar("gotenberg", cfg.GotenbergURL, gotenbergClient.Ping); err != nil {
		return err
	}
	docTools := []docgen.Generator{
		docgen.TextGenerator{},
		docgen.NewPDFGenerator(gotenbergClient),
		docgen.XLSXGenerator{},
		docgen.DOCXGenerator{},
		docgen.PPTXGenerator{},
	}
	var imageTools []imagegen.Tool
	if imageGenConfigured(cfg) {
		imageProvider := imagegen.NewFalClient(imagegen.FalConfig{
			BaseURL:     cfg.ImageGenBaseURL,
			APIKey:      cfg.ImageGenAPIKey,
			Model:       cfg.ImageGenModel,
			PollTimeout: cfg.ImageGenPollTimeout,
		})
		imageTools = append(imageTools, imagegen.NewTool(imageProvider))
	}
	// The Python sandbox is optional and never blocks boot: its health probe
	// (a worker below) offers run_python while the sidecar answers.
	sandboxRunner, sandboxWatch := sandboxForConfig(cfg)
	var chatClient httpapi.ChatClient
	if llmClient != nil {
		chatClient = llmClient
	}
	var toolService httpapi.ToolService
	// mcpRefresh keeps the servers' tool lists current (see Service.RunRefresh).
	mcpRefresh := func(context.Context) {}
	toolCfg, err := toolConfigForConfig(cfg, fetchPDF)
	if err != nil {
		return err
	}
	mcpConfig := toolCfg.union()
	if len(mcpConfig.Servers) > 0 {
		// Give discovery room for cold MCP sidecars: the fetch bridge boots a
		// Python interpreter on its first tools/list, and browser sidecars can be
		// slow on cold starts. The per-request and overall budgets are aligned so
		// a single slow server can actually use its window. Built-in servers are
		// required (boot fails if any fail); file-defined servers are best-effort
		// (a failure is logged and the server dropped) so a third-party outage or
		// exhausted quota cannot block startup.
		discoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		discovered, err := mcp.NewServiceFromConfigs(discoveryCtx, toolCfg.Required, toolCfg.FileServers, &http.Client{Timeout: 30 * time.Second}, slog.Default())
		cancel()
		if err != nil {
			return err
		}
		toolService = discovered
		mcpRefresh = func(ctx context.Context) { discovered.RunRefresh(ctx, slog.Default()) }
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
			// The dev identity is fixed by config, so its email is as trustworthy
			// as the process that set it.
			EmailVerified: true,
		}
	}
	logStartupCapabilities(cfg, mcpConfig, startupRuntime{
		DocToolCount:        len(docTools),
		ImageToolCount:      len(imageTools),
		DiscoveredToolCount: discoveredTools,
	})

	// Post-turn refreshes outlive their request; serve() drains this group before
	// the deferred db.Close above runs.
	bg := background.New(context.Background())
	// Every request derives from base; serve() cancels it on shutdown, which
	// also ends the chat turns that run detached from their client.
	base, cancelBase := context.WithCancelCause(context.Background())
	defer cancelBase(nil)
	deps := httpapi.Deps{
		Background:                 bg,
		Lifetime:                   base,
		Model:                      chatModelInfo(cfg),
		Static:                     web.SPAHandler(),
		OIDC:                       oidcService,
		Auth:                       authMW,
		Sessions:                   sessionStore,
		SessionTTL:                 cfg.SessionTTL,
		Users:                      userStore,
		Thread:                     threadStore,
		Usage:                      usageStore,
		Artifacts:                  artifactStore,
		Documents:                  documentService,
		LLM:                        chatClient,
		MCP:                        toolService,
		DocTools:                   docTools,
		ImageTools:                 imageTools,
		Sandbox:                    sandboxRunner,
		ImageDefaultModel:          cfg.ImageGenModel,
		ImageGenTypographyModel:    cfg.ImageGenTypographyModel,
		UsersDir:                   cfg.UsersDir,
		OIDCAdminGroup:             cfg.OIDC.AdminGroup,
		DevAuthClaims:              devAuthClaims,
		PostLogoutRedirectURL:      cfg.OIDC.PostLogoutRedirectURL,
		PublicURL:                  cfg.PublicURL,
		KnowledgeInlineTokenBudget: cfg.KnowledgeInlineTokenBudget,
		ProjectSummaryTokenBudget:  cfg.ProjectSummaryTokenBudget,
	}
	handler, memoryWorker := httpapi.NewWithMemoryWorker(deps)

	srv := httpx.NewServer(cfg.Addr, handler)
	srv.BaseContext = func(net.Listener) context.Context { return base }
	// Bind synchronously so "address in use" fails startup here instead of
	// being logged from a goroutine while the process waits for a signal.
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}

	ctx, stop := run.Context(context.Background())
	defer stop()
	return serve(ctx, srv, ln, bg, cancelBase, memoryWorker.Run, memoryWorker.ResumeQueuedTurns, func(ctx context.Context) {
		sessionStore.RunJanitor(ctx, sessionJanitorInterval)
	}, sandboxWatch, reembed, mcpRefresh)
}

// sandboxProbeInterval is how quickly run_python follows the sidecar going
// down or coming back.
const sandboxProbeInterval = time.Minute

// sandboxForConfig builds the run_python client when BACKEND_SANDBOX_URL is
// set. The runner is nil otherwise (never a typed nil inside the interface),
// and the watch worker then returns at once.
func sandboxForConfig(cfg config.Config) (httpapi.SandboxRunner, func(context.Context)) {
	if cfg.SandboxURL == "" {
		return nil, func(context.Context) {}
	}
	client := sandbox.New(cfg.SandboxURL, cfg.SandboxToken, cfg.SandboxTimeout)
	return client, func(ctx context.Context) { client.Watch(ctx, sandboxProbeInterval) }
}

// sessionJanitorInterval is how often expired sessions are purged while the
// server runs; boot runs one sweep as well.
const sessionJanitorInterval = time.Hour

// shutdownTimeout bounds each stage of the graceful stop: in-flight requests
// get this long to finish once a signal arrives, then the background group
// gets the same.
const shutdownTimeout = 10 * time.Second

// shutdownGrace is how long in-flight requests get to finish on their own
// before the streams among them are cancelled.
const shutdownGrace = 2 * time.Second

// errServerShuttingDown is the cause every in-flight request context is
// canceled with at shutdown, so a stream logs it as such and not as a client
// disconnect.
var errServerShuttingDown = errors.New("server shutting down")

// serve accepts on ln until ctx is done or the listener fails, then stops in
// the order the deferred db.Close in runServer() needs:
//
//  1. cancel every request context with errServerShuttingDown, so an open SSE
//     stream unwinds now (persisting its partial answer on a detached context)
//     instead of holding Shutdown for the whole timeout;
//  2. Shutdown the listener and wait for handlers to return;
//  3. drain the background group, whose tasks still write to the database;
//  4. wait for the workers to return.
//
// cancelBase cancels srv's BaseContext, which every request derives from.
// workers are the long-running sweeps (memory refresh, session janitor,
// re-embed); they stop when ctx does.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, bg *background.Group, cancelBase context.CancelCauseFunc, workers ...func(context.Context)) error {
	defer cancelBase(nil)

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", ln.Addr().String(), "version", version.String())
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	workerCtx, stopWorkers := context.WithCancel(ctx)
	var running sync.WaitGroup
	for _, worker := range workers {
		running.Go(func() { worker(workerCtx) })
	}
	// Workers write to the database, so serve returns only once they are done,
	// on every way out. On shutdown they unwind alongside the background drain
	// and share its deadline rather than adding one of their own.
	var drainBy time.Time
	defer func() {
		stopWorkers()
		if drainBy.IsZero() {
			drainBy = time.Now().Add(shutdownTimeout)
		}
		stopped := make(chan struct{})
		go func() { running.Wait(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(time.Until(drainBy)):
			slog.Warn("workers did not stop", "timeout", shutdownTimeout.String())
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("serve on %s: %w", ln.Addr().String(), err)
		}
	}

	// Phase one: stop accepting and give in-flight requests a short grace to
	// finish on their own; a thread create mid-transaction completes instead
	// of failing with a cancelled context. Phase two, only when something is
	// still running at the end of the grace (an SSE stream): cancel every
	// request context with the shutdown cause so the streams unwind, and
	// wait for them.
	graceCtx, cancelGrace := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancelGrace()
	if err := srv.Shutdown(graceCtx); err != nil {
		cancelBase(errServerShuttingDown)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("shutdown did not finish cleanly", "err", err)
		}
	}
	stopWorkers()
	drainBy = time.Now().Add(shutdownTimeout)
	if err := bg.Stop(shutdownTimeout); err != nil {
		slog.Warn("background tasks did not drain", "err", err)
	}
	return nil
}

// sidecarReadinessTimeout bounds each startup health probe. Gotenberg and Tika
// are declared service_healthy dependencies in compose, so under Docker they are
// already up by the time loom boots; this window only needs to absorb a brief
// restart race, not a cold start.
const sidecarReadinessTimeout = 15 * time.Second

// requireSidecar probes an essential sidecar's health endpoint at startup and
// returns an error (aborting boot) when it is unreachable. This is a defensive,
// in-process guard on top of compose's depends_on: service_healthy — it keeps the
// backend from coming up able to accept work (PDF export, document extraction) it
// cannot fulfil, failing fast with a clear cause instead of opaque per-request
// errors. It does not address slow/unavailable upstreams once serving.
func requireSidecar(name, url string, ping func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), sidecarReadinessTimeout)
	defer cancel()
	if err := ping(ctx); err != nil {
		return fmt.Errorf("%s sidecar not reachable at %s: %w", name, url, err)
	}
	slog.Info("sidecar ready", "sidecar", name, "url", url)
	return nil
}

func responseLogDirForConfig(cfg config.Config) string {
	if cfg.AuthMode != config.AuthModeDev {
		return ""
	}
	return cfg.ChatLogDir
}

func chatClientConfigFromConfig(cfg config.Config) llm.Config {
	return llm.Config{
		Models:              cfg.ChatModels.Roles,
		MaxCompletionTokens: cfg.ChatMaxCompletionTokens,
		Timeout:             cfg.ChatTimeout,
		IdleTimeout:         cfg.ChatIdleTimeout,
		ResponseLogDir:      responseLogDirForConfig(cfg),
	}
}

// toolServerConfig splits MCP servers by discovery policy: Required servers are
// the curated built-ins loom's boot depends on (discovery failure is fatal);
// FileServers come from the optional JSON file and are discovered best-effort.
type toolServerConfig struct {
	Required    mcp.Config
	FileServers mcp.Config
}

// union merges both sets (file entries win on a name collision) for the startup
// summary and the "any servers configured?" guard.
func (t toolServerConfig) union() mcp.Config {
	out := mcp.Config{Servers: make(map[string]mcp.ServerConfig, len(t.Required.Servers)+len(t.FileServers.Servers))}
	for name, sc := range t.Required.Servers {
		out.Servers[name] = sc
	}
	for name, sc := range t.FileServers.Servers {
		out.Servers[name] = sc
	}
	return out
}

// pdf is the fetch tool's PDF extractor: the boot-checked Tika client when
// document RAG runs, else nil (PDFs then come back unextracted).
func toolConfigForConfig(cfg config.Config, pdf mcp.PDFExtractor) (toolServerConfig, error) {
	required := mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	// Fetch runs in-process (no sidecar), so it is always available. Fetched
	// PDFs are parsed in the Tika sidecar (pdf), never in the backend.
	required.Servers["fetch"] = mcp.FetchServerConfig(pdf)
	if strings.TrimSpace(cfg.ObscuraMCPURL) != "" {
		required.Servers["obscura"] = mcp.ObscuraServerConfig(cfg.ObscuraMCPURL)
	}
	if strings.TrimSpace(cfg.TavilyAPIKey) != "" {
		required.Servers["tavily"] = mcp.TavilyServerConfig(cfg.TavilyURL, cfg.TavilyAPIKey)
	}
	// Servers from the optional JSON file are best-effort and override a built-in
	// of the same name (the built-in is dropped from the required set so user
	// configuration always wins, and a flaky override can't fail boot).
	fileServers, err := mcp.LoadServersFromFile(cfg.MCPServersFile, os.LookupEnv)
	if err != nil {
		return toolServerConfig{}, err
	}
	out := toolServerConfig{Required: required, FileServers: mcp.Config{Servers: map[string]mcp.ServerConfig{}}}
	for name, server := range fileServers {
		if _, exists := required.Servers[name]; exists {
			slog.Warn("MCP servers file overrides built-in server", "server", name)
			delete(required.Servers, name)
		}
		out.FileServers.Servers[name] = server
	}
	return out, nil
}

func tavilyConfigured(cfg config.Config) bool {
	return strings.TrimSpace(cfg.TavilyAPIKey) != ""
}

func imageGenConfigured(cfg config.Config) bool {
	return strings.TrimSpace(cfg.ImageGenAPIKey) != ""
}
