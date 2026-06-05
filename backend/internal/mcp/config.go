// Package mcp contains Slop's MCP client configuration and tool registry.
package mcp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	TransportStreamableHTTP = "streamable-http"
	TransportStdio          = "stdio"
	// defaultTavilyURL is the hosted Tavily MCP endpoint used by the built-in
	// Tavily web-search adapter when SLOP_TAVILY_URL is unset.
	defaultTavilyURL = "https://mcp.tavily.com/mcp/"
	// tavilySearchToolName is the server-side name of Tavily's web search tool.
	// It is the only tool the built-in adapter exposes; Tavily's other tools
	// (extract/map/crawl) are filtered out via the ServerConfig.Tools allowlist.
	tavilySearchToolName = "tavily_search"
)

// Config is the on-disk MCP server configuration loaded from mcp.json.
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
}

func LoadConfig(path string) (Config, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode MCP config: %w", err)
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]ServerConfig{}
	}
	for name, server := range cfg.Servers {
		normalized, err := normalizeServerConfig(name, server)
		if err != nil {
			return Config{}, err
		}
		cfg.Servers[name] = normalized
	}
	return cfg, nil
}

func normalizeServerConfig(name string, server ServerConfig) (ServerConfig, error) {
	if strings.TrimSpace(name) == "" {
		return ServerConfig{}, fmt.Errorf("MCP server name is required")
	}
	switch server.Transport {
	case "", "http", TransportStreamableHTTP:
		server.Transport = TransportStreamableHTTP
		if strings.TrimSpace(server.URL) == "" {
			return ServerConfig{}, fmt.Errorf("MCP server %q requires url", name)
		}
	case TransportStdio:
		if strings.TrimSpace(server.Command) == "" {
			return ServerConfig{}, fmt.Errorf("MCP server %q requires command", name)
		}
	default:
		return ServerConfig{}, fmt.Errorf("MCP server %q has unsupported transport %q", name, server.Transport)
	}
	if server.Headers == nil {
		server.Headers = map[string]string{}
	}
	if server.Env == nil {
		server.Env = map[string]string{}
	}
	return server, nil
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

// TavilyServerConfig builds the synthetic MCP server config for Slop's
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
		Tools:     []string{tavilySearchToolName},
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

func Context7ServerConfig(url, apiKey string) ServerConfig {
	return ServerConfig{
		Transport: TransportStreamableHTTP,
		URL:       url,
		Headers:   map[string]string{"CONTEXT7_API_KEY": apiKey},
	}
}
