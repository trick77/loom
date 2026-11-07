package mcp

import "strings"

// DocgenServer is the document tools' server (docs/docgen.md) as loom
// addresses it. The loom-docgen sidecar serves it from package docgen; its
// tests hold this to that package's name.
const DocgenServer = "docgen"

// DocgenServerConfig is the loom-docgen sidecar's endpoint: loom's own
// server, allowed to make artifacts. Best-effort like imagegen: a dead
// sidecar costs the document tools, never boot.
func DocgenServerConfig(baseURL, token string) ServerConfig {
	return ServerConfig{
		Transport: TransportStreamableHTTP,
		URL:       strings.TrimRight(baseURL, "/") + "/mcp",
		Headers:   map[string]string{"Authorization": "Bearer " + token},
		Artifacts: true,
		BuiltIn:   true,
	}
}
