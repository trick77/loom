package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/trick77/loom/internal/llm"
)

// Server origin labels reported by ServerStatus: a built-in server is wired from
// first-class app settings, a file server comes from the mcp.json file (and is
// best-effort — an unreachable one degrades instead of failing boot).
const (
	OriginBuiltIn = "built-in"
	OriginFile    = "file"
)

const (
	// statusProbeTimeout bounds each per-server reachability probe in ServerStatus.
	statusProbeTimeout = 3 * time.Second
	// requiredDiscoveryRetryInterval lets required startup discovery wait out
	// short sidecar bind races without delaying callers that pass no deadline.
	requiredDiscoveryRetryInterval = 200 * time.Millisecond
)

// Service routes tool calls to registered MCP clients and enumerates tools for the model prompt.
type Service struct {
	tools      []llm.Tool
	routes     map[string]toolRoute
	cfg        Config
	origins    map[string]string
	httpClient *http.Client

	// statusMu guards the ServerStatus cache. A status call probes every server
	// with a fresh client (a process, for stdio servers) and is reachable by any
	// signed-in user through the /mcp and /tools slash commands, so the result
	// is reused for statusCacheTTL.
	statusMu    sync.Mutex
	statusAt    time.Time
	statusCache []ServerStatus
}

// statusCacheTTL is how long a ServerStatus result is reused. The panel may
// show a server up to this much later than it changed state, which is a fair
// trade against spawning a probe per keystroke.
const statusCacheTTL = 30 * time.Second

type toolRoute struct {
	client Client
	name   string
}

// ServerStatus reports a configured MCP server's live reachability and metadata.
// Endpoint is credential-free (host for HTTP, command for stdio) — headers and
// tokens are never included. Error carries the probe failure reason when a
// server is unreachable.
type ServerStatus struct {
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	Transport string `json:"transport"`
	Endpoint  string `json:"endpoint"`
	Origin    string `json:"origin"`
	ToolCount int    `json:"toolCount"`
	Error     string `json:"error,omitempty"`
}

// ServerStatus live-probes every configured MCP server with a bounded timeout
// and reports reachability plus metadata. It uses a fresh client per probe so a
// server that recovered after a failed startup is reported active again (the
// routing clients cache their first init result and never recover). ToolCount is
// the number of tools currently exposed to the model, so a server that recovered
// post-startup can read active with a zero count until the next restart.
func (s *Service) ServerStatus(ctx context.Context) []ServerStatus {
	if s == nil || len(s.cfg.Servers) == 0 {
		return nil
	}
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if s.statusCache != nil && time.Since(s.statusAt) < statusCacheTTL {
		return append([]ServerStatus(nil), s.statusCache...)
	}
	statuses := s.probeAll(ctx)
	s.statusCache = statuses
	s.statusAt = time.Now()
	return append([]ServerStatus(nil), statuses...)
}

// probeAll probes every configured server concurrently and returns their
// statuses sorted by name.
func (s *Service) probeAll(ctx context.Context) []ServerStatus {
	names := sortedServerNames(s.cfg.Servers)

	counts := s.toolCounts()
	statuses := make([]ServerStatus, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			active, probeErr := s.probeServer(ctx, name)
			sc := s.cfg.Servers[name]
			origin := s.origins[name]
			if origin == "" {
				origin = OriginBuiltIn
			}
			statuses[i] = ServerStatus{
				Name:      name,
				Active:    active,
				Transport: sc.Transport,
				Endpoint:  endpointForServer(sc),
				Origin:    origin,
				ToolCount: counts[name],
				Error:     probeErr,
			}
		}(i, name)
	}
	wg.Wait()
	return statuses
}

// probeServer reports whether a server is reachable and, when it is not, the
// failure reason (already credential-scrubbed by the client's error path).
func (s *Service) probeServer(ctx context.Context, name string) (bool, string) {
	client := clientForServer(name, s.cfg.Servers[name], s.httpClient)
	defer func() { _ = client.Close() }()
	probeCtx, cancel := context.WithTimeout(ctx, statusProbeTimeout)
	defer cancel()
	var err error
	if probe, ok := client.(interface{ Probe(context.Context) error }); ok {
		err = probe.Probe(probeCtx)
	} else {
		_, err = client.ListTools(probeCtx)
	}
	if err != nil {
		// The reason is surfaced verbatim in the (auth-only) /mcp status panel, so
		// scrub credentials at this boundary regardless of which client path produced
		// the error.
		return false, scrubURLError(err).Error()
	}
	return true, ""
}

// toolCounts tallies how many exposed tools each server currently contributes,
// derived from the serverName__toolName exposed-name convention.
func (s *Service) toolCounts() map[string]int {
	counts := make(map[string]int, len(s.cfg.Servers))
	for _, t := range s.tools {
		if server, _, ok := SplitExposedToolName(t.Function.Name); ok {
			counts[server]++
		}
	}
	return counts
}

// endpointForServer returns a display-safe endpoint with no credentials: the
// host for HTTP servers (url.Host excludes any userinfo, and path/query are
// dropped) and the command for stdio servers. Headers/tokens are never exposed.
func endpointForServer(sc ServerConfig) string {
	if sc.Transport == TransportStdio {
		return sc.Command
	}
	if u, err := url.Parse(sc.URL); err == nil && u.Host != "" {
		return u.Host
	}
	// A scheme-less or opaque URL (e.g. "admin:pw@host:port/path") parses with an
	// empty Host and its user:pass@ folded into the opaque/scheme, so returning it
	// verbatim would leak credentials into the panel. Re-parse with a synthetic
	// scheme so the authority — including any userinfo — is recognised and dropped,
	// keeping only the host.
	if u, err := url.Parse("http://" + strings.TrimPrefix(sc.URL, "//")); err == nil && u.Host != "" {
		return u.Host
	}
	// Last resort: strip any userinfo and query string by hand so credentials are
	// never surfaced.
	rest := stripURLUserinfo(sc.URL)
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// NewService creates a Service from a map of MCP clients, discovering all tools from each client.
func NewService(clients map[string]Client) (*Service, error) {
	service := &Service{routes: map[string]toolRoute{}}
	names := sortedServerNames(clients)
	for _, serverName := range names {
		client := clients[serverName]
		tools, err := client.ListTools(context.Background())
		if err != nil {
			return nil, fmt.Errorf("list MCP tools for %s: %w", serverName, err)
		}
		if err := service.register(serverName, client, tools, failOnDuplicate); err != nil {
			return nil, err
		}
	}
	return service, nil
}

// NewServiceFromConfig creates a Service from a Config, with best-effort client initialization.
func NewServiceFromConfig(cfg Config, httpClient *http.Client) (*Service, error) {
	clients := map[string]Client{}
	for name, server := range cfg.Servers {
		clients[name] = clientForServer(name, server, httpClient)
	}
	service, err := NewService(clients)
	if err != nil {
		return nil, err
	}
	service.cfg = cfg
	service.httpClient = httpClient
	return service, nil
}

// NewRequiredServiceFromConfig creates a Service from a Config, failing if any client's tool discovery fails.
func NewRequiredServiceFromConfig(ctx context.Context, cfg Config, httpClient *http.Client) (*Service, error) {
	clients := map[string]Client{}
	for name, server := range cfg.Servers {
		clients[name] = clientForServer(name, server, httpClient)
	}
	service, err := NewRequiredServiceFromClients(ctx, clients)
	if err != nil {
		return nil, err
	}
	service.cfg = cfg
	service.httpClient = httpClient
	return service, nil
}

// NewRequiredServiceFromClients creates a Service from clients, failing if any client's tool discovery fails.
func NewRequiredServiceFromClients(ctx context.Context, clients map[string]Client) (*Service, error) {
	service := &Service{routes: map[string]toolRoute{}}
	names := sortedServerNames(clients)
	for _, serverName := range names {
		client := clients[serverName]
		tools, err := listToolsRequired(ctx, client)
		if err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("list MCP tools for %s: %w", serverName, err)
		}
		if err := service.register(serverName, client, tools, failOnDuplicate); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func listToolsRequired(ctx context.Context, client Client) ([]Tool, error) {
	_, hasDeadline := ctx.Deadline()
	tools, err := client.ListTools(ctx)
	if err == nil || !hasDeadline {
		return tools, err
	}
	for {
		timer := time.NewTimer(requiredDiscoveryRetryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, err
		case <-timer.C:
		}
		tools, nextErr := client.ListTools(ctx)
		if nextErr == nil {
			return tools, nil
		}
		err = nextErr
	}
}

// NewServiceFromConfigs discovers two sets of servers into one Service: required
// servers fail the whole construction if any of them fails discovery (used for
// the built-in sidecars/remotes loom's boot depends on), while best-effort
// servers are merely logged and dropped on discovery failure (used for
// file-defined third-party servers, so an unreachable/expired/quota-exhausted
// one degrades gracefully instead of blocking startup). On a tool-name collision
// the already-registered route wins and the duplicate is skipped — required
// servers are processed first, so a file server cannot shadow a built-in tool.
func NewServiceFromConfigs(ctx context.Context, required, bestEffort Config, httpClient *http.Client, logger *slog.Logger) (*Service, error) {
	service, err := NewRequiredServiceFromConfig(ctx, required, httpClient)
	if err != nil {
		return nil, err
	}
	names := sortedServerNames(bestEffort.Servers)
	for _, serverName := range names {
		client := clientForServer(serverName, bestEffort.Servers[serverName], httpClient)
		tools, err := client.ListTools(ctx)
		if err != nil {
			if logger != nil {
				logger.Warn("MCP server discovery failed", "server", serverName, "err", err)
			}
			_ = client.Close()
			continue
		}
		_ = service.register(serverName, client, tools, skipDuplicate(logger))
	}
	// Union the configs so ServerStatus live-probes best-effort servers too, and
	// record each server's origin so status can label built-in vs file-defined.
	merged := Config{Servers: make(map[string]ServerConfig, len(required.Servers)+len(bestEffort.Servers))}
	origins := make(map[string]string, len(required.Servers)+len(bestEffort.Servers))
	for name, sc := range required.Servers {
		merged.Servers[name] = sc
		origins[name] = OriginBuiltIn
	}
	for name, sc := range bestEffort.Servers {
		merged.Servers[name] = sc
		origins[name] = OriginFile
	}
	service.cfg = merged
	service.origins = origins
	service.httpClient = httpClient
	return service, nil
}

// NewBestEffortServiceFromConfig creates a Service from a Config, logging and skipping any server whose discovery fails.
func NewBestEffortServiceFromConfig(ctx context.Context, cfg Config, httpClient *http.Client, logger *slog.Logger) (*Service, error) {
	origins := make(map[string]string, len(cfg.Servers))
	for name := range cfg.Servers {
		origins[name] = OriginFile
	}
	service := &Service{routes: map[string]toolRoute{}, cfg: cfg, origins: origins, httpClient: httpClient}
	names := sortedServerNames(cfg.Servers)
	for _, serverName := range names {
		client := clientForServer(serverName, cfg.Servers[serverName], httpClient)
		tools, err := client.ListTools(ctx)
		if err != nil {
			if logger != nil {
				logger.Warn("MCP server discovery failed", "server", serverName, "err", err)
			}
			_ = client.Close()
			continue
		}
		if err := service.register(serverName, client, tools, failOnDuplicate); err != nil {
			return nil, err
		}
	}
	return service, nil
}

func clientForServer(name string, server ServerConfig, httpClient *http.Client) Client {
	switch server.Transport {
	case TransportInProcess:
		return NewFetchClient(name)
	case TransportStdio:
		return NewStdioClient(name, server)
	default:
		return NewRemoteClient(name, server, httpClient)
	}
}

// Tools returns the list of all discovered MCP tools.
func (s *Service) Tools() []llm.Tool {
	if s == nil {
		return nil
	}
	return append([]llm.Tool(nil), s.tools...)
}

// ToolsFor returns the exposed tools whose server is relevant to the given active
// category set. A server that declares no Categories is category-neutral and
// always included (the safe default that keeps generically-useful servers like
// web search on for every turn); a server that declares Categories is included
// only when the active set contains one of them. A tool whose server is unknown
// to the config is included, so a missing config never silently drops tools.
// Passing an empty active set therefore yields only the category-neutral servers.
func (s *Service) ToolsFor(active map[string]bool) []llm.Tool {
	if s == nil {
		return nil
	}
	out := make([]llm.Tool, 0, len(s.tools))
	for _, t := range s.tools {
		server, _, ok := SplitExposedToolName(t.Function.Name)
		if !ok {
			out = append(out, t)
			continue
		}
		cats := s.cfg.Servers[server].Categories
		if len(cats) == 0 || anyCategoryActive(cats, active) {
			out = append(out, t)
		}
	}
	return out
}

func anyCategoryActive(cats []string, active map[string]bool) bool {
	for _, c := range cats {
		if active[c] {
			return true
		}
	}
	return false
}

// HasTool reports whether an exposed tool with the given name is registered.
func (s *Service) HasTool(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.routes[name]
	return ok
}

// CallTool invokes an MCP tool by its exposed name, routing to the appropriate client.
func (s *Service) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	route, ok := s.routes[name]
	if !ok {
		return "", fmt.Errorf("unknown MCP tool %q", name)
	}
	return route.client.CallTool(ctx, route.name, arguments)
}

// sortedServerNames returns a map's keys in a stable order, so discovery and
// registration run (and log) in the same order every boot.
func sortedServerNames[V any](servers map[string]V) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// register exposes a server's tools to the model. onDuplicate decides what a
// tool name already taken by another server means: an error for a required
// server (the operator's configuration is contradictory), a logged skip for a
// best-effort one (the built-in keeps the name).
func (s *Service) register(serverName string, client Client, tools []Tool, onDuplicate func(serverName, tool string) error) error {
	for _, tool := range tools {
		if _, exists := s.routes[tool.Name]; exists {
			if err := onDuplicate(serverName, tool.Name); err != nil {
				return err
			}
			continue
		}
		s.routes[tool.Name] = toolRoute{client: client, name: tool.OriginalName}
		s.tools = append(s.tools, llm.Tool{
			Type: "function",
			Function: llm.ToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return nil
}

func failOnDuplicate(_, tool string) error {
	return fmt.Errorf("duplicate MCP tool name %q", tool)
}

func skipDuplicate(logger *slog.Logger) func(serverName, tool string) error {
	return func(serverName, tool string) error {
		if logger != nil {
			logger.Warn("skipping duplicate MCP tool name", "tool", tool, "server", serverName)
		}
		return nil
	}
}
