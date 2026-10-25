// Package mcp contains Loom's MCP client configuration and tool registry.
package mcp

import (
	"context"
	"net/url"
	"strings"
)

const (
	// TransportStreamableHTTP denotes the Streamable HTTP protocol for MCP servers.
	TransportStreamableHTTP = "streamable-http"
	// TransportStdio denotes stdio-based MCP servers.
	TransportStdio = "stdio"
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
	// injected. Values are opaque strings here — the turn engine maps them to
	// its classifier categories — so an unrecognized value simply never matches
	// and hides the server. See Service.ToolsFor.
	Categories []string `json:"categories"`
	// RestrictUsers limits the server's tools to the loom accounts in Users:
	// the server reads one person's data (a personal library behind one token),
	// which other accounts on this loom must not reach. An empty Users with
	// RestrictUsers set serves nobody: an unset allowlist fails closed.
	RestrictUsers bool `json:"restrictUsers"`
	// Users holds loom user ids, the one key that never changes: the
	// username is the IdP's mutable preferred_username (or email) and is not
	// unique, and the stored OIDC subject is rewritten when an account is
	// adopted by verified email.
	Users []string `json:"users"`
	// TrustInstructions lets the server's own instructions into the system
	// prompt while its tools are offered. Off by default: a third-party server
	// could change them at any refresh.
	TrustInstructions bool `json:"trustInstructions"`
	// PDFExtractor, for the in-process fetch server only, turns a fetched PDF
	// into text outside the backend process. Nil leaves PDFs unextracted.
	PDFExtractor PDFExtractor `json:"-"`
}

// PDFExtractor extracts the text of a fetched PDF (webfetch.Options.PDFHandler).
type PDFExtractor func(ctx context.Context, body []byte) (string, error)

// Principal names a loom account to a users allowlist by its user id.
type Principal struct {
	ID string
}

// UsersAllow reports whether a users allowlist admits the account: an exact
// match on the opaque user id. It is the single definition, shared with the
// turn test fake so the two cannot drift.
func UsersAllow(users []string, p Principal) bool {
	for _, allowed := range users {
		if allowed != "" && allowed == p.ID {
			return true
		}
	}
	return false
}

// userAllowed reports whether the account may use this server's tools.
func (cfg ServerConfig) userAllowed(p Principal) bool {
	return !cfg.RestrictUsers || UsersAllow(cfg.Users, p)
}

// ExposedToolName constructs the exposed tool name by joining server and tool names with "__".
func ExposedToolName(serverName, toolName string) string {
	return serverName + "__" + toolName
}

// SplitExposedToolName parses an exposed tool name into its server and tool components.
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
// ("fetch__fetch") are unchanged. pdf may be nil.
func FetchServerConfig(pdf PDFExtractor) ServerConfig {
	return ServerConfig{
		Transport:    TransportInProcess,
		Tools:        []string{"fetch"},
		PDFExtractor: pdf,
	}
}

// ObscuraServerConfig builds the config for the headless-browser sidecar. The
// Tools allowlist deliberately exposes only navigate + snapshot: those are the
// two tools the deterministic fetch->obscura fallback drives (see
// obscuraNavigateToolName/obscuraSnapshotToolName in the turn package), and
// they cover read-style browsing. The full obscura surface (~20 interactive
// browser tools: click/type/form-fill/evaluate/...) is not injected — it would
// dominate the prompt's tool budget on every turn and Loom's flows do not drive
// interactive browser automation.
//
// token is the sidecar's OBSCURA_MCP_TOKEN, sent as a bearer token: obscura
// v0.2.3+ refuses to serve MCP beyond loopback without one and answers 401 to
// a request that lacks it (config.Load requires one with the URL).
func ObscuraServerConfig(url, token string) ServerConfig {
	return ServerConfig{
		Transport: TransportStreamableHTTP,
		URL:       url,
		Headers:   map[string]string{"Authorization": "Bearer " + token},
		Tools:     []string{"browser_navigate", "browser_snapshot"},
	}
}
