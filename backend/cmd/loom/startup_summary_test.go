package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/trick77/llmwire/llmwiretest"
	"github.com/trick77/loom/internal/config"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// testChatModels resolves the roles onto llmwiretest's synthetic model, so no
// test names a real one.
func testChatModels(t *testing.T) llm.Resolved {
	t.Helper()
	resolved, err := llm.ResolveRoles(llmwiretest.Registry(), llm.Roles{Chat: llmwiretest.ChatModel})
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// Outside dev auth a chat model or key that is missing is a broken deployment,
// not a routine capability line; the warning names what is missing.
func TestLogStartupCapabilitiesWarnsWithoutChatKeyOutsideDev(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	logStartupCapabilities(config.Config{AuthMode: config.AuthModeOIDC, ChatMissing: "LLMWIRE_EXAMPLE_API_KEY"}, mcp.Config{}, startupRuntime{})
	if !strings.Contains(buf.String(), "level=WARN msg=\"chat disabled: LLMWIRE_EXAMPLE_API_KEY is unset") {
		t.Fatalf("no warning without a chat key:\n%s", buf.String())
	}
	buf.Reset()
	logStartupCapabilities(config.Config{AuthMode: config.AuthModeDev, ChatMissing: "BACKEND_CHAT_MODEL"}, mcp.Config{}, startupRuntime{})
	if strings.Contains(buf.String(), "level=WARN msg=\"chat disabled") {
		t.Fatalf("dev auth must not warn:\n%s", buf.String())
	}
	buf.Reset()
	logStartupCapabilities(config.Config{AuthMode: config.AuthModeOIDC, ChatEnabled: true}, mcp.Config{}, startupRuntime{})
	if strings.Contains(buf.String(), "level=WARN") {
		t.Fatalf("a set key must not warn:\n%s", buf.String())
	}
}

func TestStartupCapabilitiesDefaultDisabledFeatures(t *testing.T) {
	items := startupCapabilities(config.Config{
		UsersDir:    "/data/users",
		TikaURL:     "http://tika:9998",
		ChatMissing: "BACKEND_CHAT_MODEL",
	}, mcp.Config{}, startupRuntime{DocToolCount: 5})

	assertCapability(t, items, "chat", "disabled", "BACKEND_CHAT_MODEL")
	assertCapability(t, items, "embeddings", "disabled", "LLMWIRE_OPENAI_API_KEY")
	assertCapability(t, items, "MCP tools", "disabled", "no configured MCP servers")
	assertCapability(t, items, "Tavily web search", "disabled", "BACKEND_TAVILY_API_KEY")
	assertCapability(t, items, "Image generation", "disabled", "BACKEND_IMAGE_GEN_API_KEY")
	assertCapability(t, items, "document generation", "enabled", "tools=5")
	assertCapability(t, items, "artifacts", "enabled", "users_dir=/data/users")
}

func TestStartupCapabilitiesEnabledByConfig(t *testing.T) {
	items := startupCapabilities(config.Config{
		AuthMode:       config.AuthModeDev,
		ChatEnabled:    true,
		ChatModels:     testChatModels(t),
		EmbedEnabled:   true,
		TikaURL:        "http://tika:9998",
		UsersDir:       "/data/users",
		TavilyAPIKey:   "tavily-key",
		ImageGenAPIKey: "fal-key",
		ImageGenModel:  "fal-ai/flux-2-pro",
		ChatLogDir:     "logs/llm-responses",
	}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		"fetch": {Transport: mcp.TransportStreamableHTTP, URL: "http://fetch:8080/mcp"},
	}}, startupRuntime{DocToolCount: 5, ImageToolCount: 1, DiscoveredToolCount: 3})

	assertCapability(t, items, "auth", "dev", "local loopback only")
	assertCapability(t, items, "chat", "enabled", "model="+llmwiretest.ChatModel)
	assertCapability(t, items, "embeddings", "enabled", "text-embedding-3-small")
	assertCapability(t, items, "MCP tools", "enabled", "servers=1 discovered_tools=3")
	assertCapability(t, items, "Tavily web search", "enabled", "source=env")
	assertCapability(t, items, "Image generation", "enabled", "model=fal-ai/flux-2-pro tools=1")
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
