package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/config"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/rag"
	"github.com/trick77/toolkit/llmwire/llmwiretest"
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
		UsersDir:     "/data/users",
		TikaURL:      "http://tika:9998",
		ChatMissing:  "BACKEND_CHAT_MODEL",
		EmbedMissing: "BACKEND_EMBED_MODEL",
	}, mcp.Config{}, startupRuntime{DocToolCount: 5})

	assertCapability(t, items, "chat", "disabled", "BACKEND_CHAT_MODEL")
	assertCapability(t, items, "embeddings", "disabled", "BACKEND_EMBED_MODEL")
	assertCapability(t, items, "MCP tools", "disabled", "no configured MCP servers")
	assertCapability(t, items, "Web search", "disabled", "BACKEND_SEARCH_URL")
	assertCapability(t, items, "Image generation", "disabled", "BACKEND_IMAGE_GEN_URL")
	assertCapability(t, items, "document generation", "enabled", "tools=5")
	assertCapability(t, items, "artifacts", "enabled", "users_dir=/data/users")
	assertCapability(t, items, "Python sandbox", "disabled", "BACKEND_SANDBOX_URL")
}

// An mcp.json server named imagegen serves images, with or without a fal key.
func TestStartupCapabilitiesImageServerFromFile(t *testing.T) {
	servers := mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.ImagegenServer: {Transport: mcp.TransportStreamableHTTP, URL: "http://imagegen:8080/mcp", Artifacts: true},
	}}
	items := startupCapabilities(config.Config{}, servers, startupRuntime{ImageToolUp: true})
	assertCapability(t, items, "Image generation", "enabled", "mcp.json server imagegen")
	// Configured but not answering discovery: not "enabled".
	items = startupCapabilities(config.Config{}, servers, startupRuntime{})
	assertCapability(t, items, "Image generation", "configured", "not answering yet")
	// Without artifacts its images are dropped: say so, not "enabled".
	items = startupCapabilities(config.Config{}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.ImagegenServer: {Transport: mcp.TransportStreamableHTTP, URL: "http://imagegen:8080/mcp"},
	}}, startupRuntime{})
	assertCapability(t, items, "Image generation", "disabled", `lacks "artifacts": true`)
}

// The search sidecar answers without a provider key but serves no tool: say
// why, not "not answering". An mcp.json server named search needs no key.
func TestStartupCapabilitiesSearch(t *testing.T) {
	sidecar := mcp.Config{Servers: map[string]mcp.ServerConfig{mcp.SearchServer: mcp.SearchServerConfig("http://search:8030", "t")}}
	items := startupCapabilities(config.Config{}, sidecar, startupRuntime{SearchToolUp: true, SearchKeySet: true})
	assertCapability(t, items, "Web search", "enabled", "loom-search sidecar")
	items = startupCapabilities(config.Config{}, sidecar, startupRuntime{})
	assertCapability(t, items, "Web search", "disabled", "set BACKEND_TAVILY_API_KEY")
	items = startupCapabilities(config.Config{}, sidecar, startupRuntime{SearchKeySet: true})
	assertCapability(t, items, "Web search", "configured", "not answering yet")

	file := mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.SearchServer: {Transport: mcp.TransportStreamableHTTP, URL: "http://my-search:8080/mcp"},
	}}
	items = startupCapabilities(config.Config{}, file, startupRuntime{SearchToolUp: true})
	assertCapability(t, items, "Web search", "enabled", "mcp.json server search")
	items = startupCapabilities(config.Config{}, file, startupRuntime{})
	assertCapability(t, items, "Web search", "configured", "not answering yet")
}

func TestStartupCapabilitiesSandboxConfigured(t *testing.T) {
	items := startupCapabilities(config.Config{SandboxURL: "http://sandbox:8070", SandboxTimeout: time.Minute}, mcp.Config{}, startupRuntime{})
	assertCapability(t, items, "Python sandbox", "configured", "url=http://sandbox:8070 timeout=1m0s")

	items = startupCapabilities(config.Config{SandboxProblem: "BACKEND_SANDBOX_URL must be an absolute http(s) URL"}, mcp.Config{}, startupRuntime{})
	assertCapability(t, items, "Python sandbox", "misconfigured", "absolute http(s) URL")
}

func TestStartupCapabilitiesEnabledByConfig(t *testing.T) {
	items := startupCapabilities(config.Config{
		AuthMode:     config.AuthModeDev,
		ChatEnabled:  true,
		ChatModels:   testChatModels(t),
		EmbedEnabled: true,
		EmbedModel:   rag.EmbedModel{ID: llmwiretest.EmbedModel},
		TikaURL:      "http://tika:9998",
		UsersDir:     "/data/users",
		ChatLogDir:   "logs/llm-responses",
	}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		"fetch":            {Transport: mcp.TransportStreamableHTTP, URL: "http://fetch:8080/mcp"},
		mcp.ImagegenServer: mcp.ImagegenServerConfig("http://imagegen:8040", "token"),
		mcp.SearchServer:   mcp.SearchServerConfig("http://search:8030", "token"),
	}}, startupRuntime{DocToolCount: 5, DiscoveredToolCount: 3, ImageToolUp: true, SearchToolUp: true, SearchKeySet: true})

	assertCapability(t, items, "auth", "dev", "local loopback only")
	assertCapability(t, items, "chat", "enabled", "model="+llmwiretest.ChatModel)
	assertCapability(t, items, "embeddings", "enabled", "model="+llmwiretest.EmbedModel)
	assertCapability(t, items, "MCP tools", "enabled", "servers=3 discovered_tools=3")
	assertCapability(t, items, "Web search", "enabled", "loom-search sidecar")
	assertCapability(t, items, "Image generation", "enabled", "loom-imagegen sidecar")
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
