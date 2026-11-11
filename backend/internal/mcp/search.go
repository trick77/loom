package mcp

import "strings"

// The web search contract (docs/search.md) as loom addresses it. The
// loom-search sidecar serves it; its tests hold these to its names.
const (
	SearchServer = "search"
	SearchTool   = "web"
)

// SearchServerConfig is the loom-search sidecar's endpoint. Best-effort like
// imagegen: a dead sidecar costs web search, never boot. Its instructions
// stay out of the prompt like any server's (the tool describes itself).
func SearchServerConfig(baseURL, token string) ServerConfig {
	return ServerConfig{
		Transport: TransportStreamableHTTP,
		URL:       strings.TrimRight(baseURL, "/") + "/mcp",
		Headers:   map[string]string{"Authorization": "Bearer " + token},
		BuiltIn:   true,
		ReadOnly:  true,
	}
}
