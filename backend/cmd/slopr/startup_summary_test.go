package main

import (
	"strings"
	"testing"

	"github.com/trick77/slopr/internal/config"
	"github.com/trick77/slopr/internal/mcp"
)

func TestStartupCapabilitiesDefaultDisabledFeatures(t *testing.T) {
	items := startupCapabilities(config.Config{
		UsersDir:   "/data/users",
		TikaURL:    "http://tika:9998",
		ChatModel:  "MiMo",
		EmbedModel: "text-embedding-3-small",
	}, mcp.Config{}, startupRuntime{DocToolCount: 5})

	assertCapability(t, items, "chat", "disabled", "SLOPR_CHAT_BASE_URL")
	assertCapability(t, items, "embeddings", "disabled", "SLOPR_EMBED_BASE_URL")
	assertCapability(t, items, "MCP tools", "disabled", "no configured MCP servers")
	assertCapability(t, items, "Tavily web search", "disabled", "SLOPR_TAVILY_API_KEY")
	assertCapability(t, items, "Context7 docs", "disabled", "SLOPR_CONTEXT7_API_KEY")
	assertCapability(t, items, "BFL image generation", "disabled", "SLOPR_BFL_API_KEY")
	assertCapability(t, items, "document generation", "enabled", "tools=5")
	assertCapability(t, items, "artifacts", "enabled", "users_dir=/data/users")
}

func TestStartupCapabilitiesEnabledByConfig(t *testing.T) {
	items := startupCapabilities(config.Config{
		AuthMode:       config.AuthModeDev,
		ChatBaseURL:    "https://chat.example/v1",
		ChatModel:      "MiMo",
		EmbedBaseURL:   "https://api.openai.com/v1",
		EmbedAPIKey:    "embed-key",
		EmbedModel:     "text-embedding-3-small",
		TikaURL:        "http://tika:9998",
		UsersDir:       "/data/users",
		TavilyAPIKey:   "tavily-key",
		Context7APIKey: "ctx-key",
		Context7MCPURL: "https://mcp.context7.com/mcp",
		BFLAPIKey:      "bfl-key",
		BFLModel:       "flux-2-klein-4b",
		ChatLogDir:     "logs/llm-responses",
	}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		"fetch": {Transport: mcp.TransportStreamableHTTP, URL: "http://fetch:8080/mcp"},
	}}, startupRuntime{DocToolCount: 5, ImageToolCount: 1, DiscoveredToolCount: 3})

	assertCapability(t, items, "auth", "dev", "local loopback only")
	assertCapability(t, items, "chat", "enabled", "model=MiMo")
	assertCapability(t, items, "embeddings", "enabled", "text-embedding-3-small")
	assertCapability(t, items, "MCP tools", "enabled", "servers=1 discovered_tools=3")
	assertCapability(t, items, "Tavily web search", "enabled", "source=env")
	assertCapability(t, items, "Context7 docs", "enabled", "source=env")
	assertCapability(t, items, "BFL image generation", "enabled", "model=flux-2-klein-4b tools=1")
	assertCapability(t, items, "LLM response logging", "enabled", "logs/llm-responses")
}

func assertCapability(t *testing.T, items []startupCapability, name, status, detailContains string) {
	t.Helper()
	for _, item := range items {
		if item.Name != name {
			continue
		}
		if item.Status != status {
			t.Fatalf("%s status = %q, want %q", name, item.Status, status)
		}
		if !strings.Contains(item.Detail, detailContains) {
			t.Fatalf("%s detail = %q, want containing %q", name, item.Detail, detailContains)
		}
		return
	}
	t.Fatalf("capability %q not found in %#v", name, items)
}
