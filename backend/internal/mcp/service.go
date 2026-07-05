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

type Service struct {
	tools      []llm.Tool
	routes     map[string]toolRoute
	cfg        Config
	origins    map[string]string
	httpClient *http.Client
}

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
	names := make([]string, 0, len(s.cfg.Servers))
	for name := range s.cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)

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
		return false, err.Error()
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
	// Fallback for a scheme-less or opaque URL that yields no host: still drop any
	// query string so a credential-bearing param is never surfaced.
	if i := strings.IndexByte(sc.URL, '?'); i >= 0 {
		return sc.URL[:i]
	}
	return sc.URL
}

func NewService(clients map[string]Client) (*Service, error) {
	service := &Service{routes: map[string]toolRoute{}}
	names := make([]string, 0, len(clients))
	for name := range clients {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, serverName := range names {
		client := clients[serverName]
		tools, err := client.ListTools(context.Background())
		if err != nil {
			return nil, fmt.Errorf("list MCP tools for %s: %w", serverName, err)
		}
		for _, tool := range tools {
			if _, exists := service.routes[tool.Name]; exists {
				return nil, fmt.Errorf("duplicate MCP tool name %q", tool.Name)
			}
			service.routes[tool.Name] = toolRoute{client: client, name: tool.OriginalName}
			service.tools = append(service.tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
	}
	return service, nil
}

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

func NewRequiredServiceFromClients(ctx context.Context, clients map[string]Client) (*Service, error) {
	service := &Service{routes: map[string]toolRoute{}}
	names := make([]string, 0, len(clients))
	for name := range clients {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, serverName := range names {
		client := clients[serverName]
		tools, err := listToolsRequired(ctx, client)
		if err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("list MCP tools for %s: %w", serverName, err)
		}
		for _, tool := range tools {
			if _, exists := service.routes[tool.Name]; exists {
				return nil, fmt.Errorf("duplicate MCP tool name %q", tool.Name)
			}
			service.routes[tool.Name] = toolRoute{client: client, name: tool.OriginalName}
			service.tools = append(service.tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
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
	names := make([]string, 0, len(bestEffort.Servers))
	for name := range bestEffort.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
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
		for _, tool := range tools {
			if _, exists := service.routes[tool.Name]; exists {
				if logger != nil {
					logger.Warn("skipping duplicate MCP tool name", "tool", tool.Name, "server", serverName)
				}
				continue
			}
			service.routes[tool.Name] = toolRoute{client: client, name: tool.OriginalName}
			service.tools = append(service.tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
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

func NewBestEffortServiceFromConfig(ctx context.Context, cfg Config, httpClient *http.Client, logger *slog.Logger) (*Service, error) {
	origins := make(map[string]string, len(cfg.Servers))
	for name := range cfg.Servers {
		origins[name] = OriginFile
	}
	service := &Service{routes: map[string]toolRoute{}, cfg: cfg, origins: origins, httpClient: httpClient}
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
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
		for _, tool := range tools {
			if _, exists := service.routes[tool.Name]; exists {
				return nil, fmt.Errorf("duplicate MCP tool name %q", tool.Name)
			}
			service.routes[tool.Name] = toolRoute{client: client, name: tool.OriginalName}
			service.tools = append(service.tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
	}
	return service, nil
}

func clientForServer(name string, server ServerConfig, httpClient *http.Client) Client {
	if server.Transport == TransportStdio {
		return NewStdioClient(name, server)
	}
	return NewRemoteClient(name, server, httpClient)
}

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

func (s *Service) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	route, ok := s.routes[name]
	if !ok {
		return "", fmt.Errorf("unknown MCP tool %q", name)
	}
	return route.client.CallTool(ctx, route.name, arguments)
}
