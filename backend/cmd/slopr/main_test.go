package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/slopr/internal/config"
	"github.com/trick77/slopr/internal/mcp"
)

func TestResponseLogDirForConfigOnlyEnablesDevMode(t *testing.T) {
	cfg := config.Config{AuthMode: config.AuthModeOIDC, ChatLogDir: "logs/llm-responses"}
	if got := responseLogDirForConfig(cfg); got != "" {
		t.Fatalf("responseLogDirForConfig(OIDC) = %q, want empty", got)
	}

	cfg.AuthMode = config.AuthModeDev
	if got := responseLogDirForConfig(cfg); got != "logs/llm-responses" {
		t.Fatalf("responseLogDirForConfig(dev) = %q, want logs/llm-responses", got)
	}
}

func TestChatClientConfigFromConfigIncludesReasoningEffort(t *testing.T) {
	cfg := config.Config{
		ChatBaseURL:             "https://chat.example/v1",
		ChatAPIKey:              "secret",
		ChatModel:               "mimo",
		ChatReasoningEffort:     "low",
		ChatMaxCompletionTokens: 4096,
		ChatTimeout:             90 * time.Second,
		ChatLogDir:              "logs/llm-responses",
		AuthMode:                config.AuthModeDev,
	}

	got := chatClientConfigFromConfig(cfg)
	if got.BaseURL != cfg.ChatBaseURL {
		t.Fatalf("BaseURL = %q, want %q", got.BaseURL, cfg.ChatBaseURL)
	}
	if got.APIKey != cfg.ChatAPIKey {
		t.Fatalf("APIKey = %q, want %q", got.APIKey, cfg.ChatAPIKey)
	}
	if got.Model != cfg.ChatModel {
		t.Fatalf("Model = %q, want %q", got.Model, cfg.ChatModel)
	}
	if got.ReasoningEffort != cfg.ChatReasoningEffort {
		t.Fatalf("ReasoningEffort = %q, want %q", got.ReasoningEffort, cfg.ChatReasoningEffort)
	}
	if got.MaxCompletionTokens != cfg.ChatMaxCompletionTokens {
		t.Fatalf("MaxCompletionTokens = %d, want %d", got.MaxCompletionTokens, cfg.ChatMaxCompletionTokens)
	}
	if got.Timeout != cfg.ChatTimeout {
		t.Fatalf("Timeout = %s, want %s", got.Timeout, cfg.ChatTimeout)
	}
	if got.ResponseLogDir != cfg.ChatLogDir {
		t.Fatalf("ResponseLogDir = %q, want %q", got.ResponseLogDir, cfg.ChatLogDir)
	}
}

func TestToolConfigForConfigAddsBuiltInTavily(t *testing.T) {
	cfg := config.Config{
		FetchMCPURL:  "http://fetch:8090/mcp",
		TavilyURL:    "https://mcp.tavily.com/mcp/",
		TavilyAPIKey: "secret",
	}

	got := toolConfigForConfig(cfg)
	if got.Servers["fetch"].URL != "http://fetch:8090/mcp" {
		t.Fatalf("fetch config = %#v", got.Servers["fetch"])
	}
	tavily := got.Servers["tavily"]
	if tavily.Transport != mcp.TransportStreamableHTTP {
		t.Fatalf("tavily transport = %q, want streamable-http", tavily.Transport)
	}
	if !strings.Contains(tavily.URL, "tavilyApiKey=secret") {
		t.Fatalf("tavily URL = %q, want embedded tavilyApiKey", tavily.URL)
	}
	if len(tavily.Tools) != 1 || tavily.Tools[0] != "tavily_search" {
		t.Fatalf("tavily tools = %#v, want [tavily_search]", tavily.Tools)
	}
}

func TestToolConfigForConfigLeavesTavilyDisabledWhenKeyIsEmpty(t *testing.T) {
	got := toolConfigForConfig(config.Config{TavilyURL: "https://mcp.tavily.com/mcp/"})
	if _, exists := got.Servers["tavily"]; exists {
		t.Fatalf("tavily server exists when SLOPR_TAVILY_API_KEY is empty: %#v", got.Servers["tavily"])
	}
}

func TestTavilyConfiguredDetectsEnv(t *testing.T) {
	if tavilyConfigured(config.Config{}) {
		t.Fatal("tavilyConfigured() = true without API key, want false")
	}
	if !tavilyConfigured(config.Config{TavilyAPIKey: "secret"}) {
		t.Fatal("tavilyConfigured() = false with API key, want true")
	}
}

func TestToolConfigForConfigAddsContext7WhenAPIKeyIsSet(t *testing.T) {
	cfg := config.Config{
		Context7APIKey: "ctx-key",
		Context7MCPURL: "https://mcp.context7.com/mcp",
	}

	got := toolConfigForConfig(cfg)
	context7 := got.Servers["context7"]
	if context7.Transport != mcp.TransportStreamableHTTP {
		t.Fatalf("context7 transport = %q, want streamable-http", context7.Transport)
	}
	if context7.URL != "https://mcp.context7.com/mcp" {
		t.Fatalf("context7 URL = %q, want Context7 remote endpoint", context7.URL)
	}
	if context7.Headers["CONTEXT7_API_KEY"] != "ctx-key" {
		t.Fatalf("context7 headers = %#v, want API key header", context7.Headers)
	}
}

func TestToolConfigForConfigAddsFirstClassMCPTools(t *testing.T) {
	cfg := config.Config{
		FetchMCPURL:   "http://fetch:8090/mcp",
		ObscuraMCPURL: "http://obscura:8090/mcp",
	}

	got := toolConfigForConfig(cfg)
	fetch := got.Servers["fetch"]
	if fetch.Transport != mcp.TransportStreamableHTTP {
		t.Fatalf("fetch transport = %q, want streamable-http", fetch.Transport)
	}
	if fetch.URL != "http://fetch:8090/mcp" {
		t.Fatalf("fetch URL = %q, want configured URL", fetch.URL)
	}
	if len(fetch.Tools) != 1 || fetch.Tools[0] != "fetch" {
		t.Fatalf("fetch tools = %#v, want [fetch]", fetch.Tools)
	}
	obscura := got.Servers["obscura"]
	if obscura.Transport != mcp.TransportStreamableHTTP {
		t.Fatalf("obscura transport = %q, want streamable-http", obscura.Transport)
	}
	if obscura.URL != "http://obscura:8090/mcp" {
		t.Fatalf("obscura URL = %q, want configured URL", obscura.URL)
	}
	if len(obscura.Tools) != 0 {
		t.Fatalf("obscura tools = %#v, want no allowlist", obscura.Tools)
	}
}

func TestContext7ConfiguredDetectsEnv(t *testing.T) {
	if context7Configured(config.Config{}) {
		t.Fatal("context7Configured() = true without API key, want false")
	}
	if !context7Configured(config.Config{Context7APIKey: "ctx-key"}) {
		t.Fatal("context7Configured() = false with API key, want true")
	}
}

func TestHealthcheckURLUsesLoopbackForWildcardListenAddress(t *testing.T) {
	if got, want := healthcheckURL(":8080"), "http://127.0.0.1:8080/api/health"; got != want {
		t.Fatalf("healthcheckURL(:8080) = %q, want %q", got, want)
	}
}

func TestRunHealthcheckRequiresHealthyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Fatalf("path = %q, want /api/health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := runHealthcheck(server.URL + "/api/health"); err != nil {
		t.Fatalf("runHealthcheck() error = %v", err)
	}
}

func TestRunHealthcheckFailsUnhealthyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if err := runHealthcheck(server.URL + "/api/health"); err == nil {
		t.Fatal("runHealthcheck() error = nil, want failure")
	}
}
