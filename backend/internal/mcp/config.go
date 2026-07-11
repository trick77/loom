// Package mcp contains Loom's MCP client configuration and tool registry.
package mcp

import (
	"net/url"
	"strings"
)

const (
	TransportStreamableHTTP = "streamable-http"
	TransportStdio          = "stdio"
	// TransportInProcess routes a server to an in-process Client implemented in
	// Go, with no network or subprocess. Used by the built-in fetch tool.
	TransportInProcess = "in-process"
	// defaultTavilyURL is the hosted Tavily MCP endpoint used by the built-in
	// Tavily web-search adapter when BACKEND_TAVILY_URL is unset.
	defaultTavilyURL = "https://mcp.tavily.com/mcp/"
	// TavilySearchToolName is the server-side name of Tavily's web search tool.
	// It is the only tool the built-in adapter exposes; Tavily's other tools
	// (extract/map/crawl) are filtered out via the ServerConfig.Tools allowlist.
	// Exported so call sites can derive the exposed tool name via ExposedToolName
	// instead of duplicating the literal.
	TavilySearchToolName = "tavily_search"
)

// Config is the runtime MCP server configuration built from first-class app settings.
type Config struct {
	Servers map[string]ServerConfig `json:"servers"`
}

// ServerConfig describes one MCP server.
type ServerConfig struct {
	Transport string            `json:"transport"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	// Tools is an optional allowlist of server-side tool names. When non-empty,
	// only tools whose original name appears here are exposed; an empty list
	// exposes every tool the server advertises.
	Tools []string `json:"tools"`
	// Categories is an optional list of conversation classifier categories this
	// server is relevant to (e.g. ["coding"]). When non-empty, the server's tools
	// are injected into the prompt only for turns whose active category set
	// includes one of these values; an empty list is category-neutral and always
	// injected. Values are opaque strings here — the httpapi layer maps them to
	// its classifier categories — so an unrecognized value simply never matches
	// and hides the server. See Service.ToolsFor.
	Categories []string `json:"categories"`
}

func ExposedToolName(serverName, toolName string) string {
	return serverName + "__" + toolName
}

func SplitExposedToolName(name string) (string, string, bool) {
	server, tool, ok := strings.Cut(name, "__")
	if !ok || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}

// TavilyServerConfig builds the synthetic MCP server config for Loom's
// built-in Tavily web search. Auth uses Tavily's documented query parameter
// (?tavilyApiKey=...), so the key lives in the URL and must be scrubbed from any
// error before it is logged (see scrubURLError in client.go). The Tools
// allowlist restricts the exposed surface to the search tool only.
func TavilyServerConfig(baseURL, apiKey string) ServerConfig {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultTavilyURL
	}
	cfg := ServerConfig{
		Transport: TransportStreamableHTTP,
		Tools:     []string{TavilySearchToolName},
	}
	if u, err := url.Parse(baseURL); err == nil {
		q := u.Query()
		q.Set("tavilyApiKey", apiKey)
		u.RawQuery = q.Encode()
		cfg.URL = u.String()
		return cfg
	}
	// A malformed base URL still carries the key so the failure surfaces as an
	// HTTP error rather than silently dropping auth.
	sep := "?"
	if strings.Contains(baseURL, "?") {
		sep = "&"
	}
	cfg.URL = baseURL + sep + "tavilyApiKey=" + url.QueryEscape(apiKey)
	return cfg
}

// FetchServerConfig builds the config for the built-in fetch tool. Fetch runs
// in-process (via the shared github.com/trick77/webfetch module) rather than as
// an external MCP sidecar, so there is no URL; the exposed tool name and surface
// ("fetch__fetch") are unchanged.
func FetchServerConfig() ServerConfig {
	return ServerConfig{
		Transport: TransportInProcess,
		Tools:     []string{"fetch"},
	}
}

// ObscuraServerConfig builds the config for the headless-browser sidecar. The
// Tools allowlist deliberately exposes only navigate + snapshot: those are the
// two tools the deterministic fetch->obscura fallback drives (see
// obscuraNavigateToolName/obscuraSnapshotToolName in the httpapi package), and
// they cover read-style browsing. The full obscura surface (~20 interactive
// browser tools: click/type/form-fill/evaluate/...) is not injected — it would
// dominate the prompt's tool budget on every turn and Loom's flows do not drive
// interactive browser automation.
func ObscuraServerConfig(url string) ServerConfig {
	return ServerConfig{
		Transport: TransportStreamableHTTP,
		URL:       url,
		Tools:     []string{"browser_navigate", "browser_snapshot"},
	}
}
