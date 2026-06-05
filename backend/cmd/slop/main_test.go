package main

import (
	"strings"
	"testing"

	"github.com/trick77/slop/internal/config"
	"github.com/trick77/slop/internal/mcp"
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
		ChatBaseURL:         "https://chat.example/v1",
		ChatAPIKey:          "secret",
		ChatModel:           "mimo",
		ChatReasoningEffort: "low",
		ChatLogDir:          "logs/llm-responses",
		AuthMode:            config.AuthModeDev,
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
	if got.ResponseLogDir != cfg.ChatLogDir {
		t.Fatalf("ResponseLogDir = %q, want %q", got.ResponseLogDir, cfg.ChatLogDir)
	}
}

func TestToolConfigForConfigAddsBuiltInTavily(t *testing.T) {
	base := mcp.Config{Servers: map[string]mcp.ServerConfig{
		"obscura": {Transport: mcp.TransportStreamableHTTP, URL: "http://obscura:8090/mcp"},
	}}
	cfg := config.Config{TavilyURL: "https://mcp.tavily.com/mcp/", TavilyAPIKey: "secret"}

	got, collision := toolConfigForConfig(cfg, base)
	if collision {
		t.Fatal("toolConfigForConfig() collision = true, want false")
	}
	if got.Servers["obscura"].URL != "http://obscura:8090/mcp" {
		t.Fatalf("obscura config = %#v", got.Servers["obscura"])
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
	got, collision := toolConfigForConfig(config.Config{TavilyURL: "https://mcp.tavily.com/mcp/"}, mcp.Config{})
	if collision {
		t.Fatal("toolConfigForConfig() collision = true, want false")
	}
	if _, exists := got.Servers["tavily"]; exists {
		t.Fatalf("tavily server exists when SLOP_TAVILY_API_KEY is empty: %#v", got.Servers["tavily"])
	}
}

func TestTavilyConfiguredDetectsEnvOrExternalServer(t *testing.T) {
	if tavilyConfigured(config.Config{}, mcp.Config{}) {
		t.Fatal("tavilyConfigured() = true without API key or external server, want false")
	}
	if !tavilyConfigured(config.Config{TavilyAPIKey: "secret"}, mcp.Config{}) {
		t.Fatal("tavilyConfigured() = false with API key, want true")
	}
	if !tavilyConfigured(config.Config{}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		"tavily": {Transport: mcp.TransportStreamableHTTP, URL: "http://custom-tavily:8080/mcp"},
	}}) {
		t.Fatal("tavilyConfigured() = false with external tavily server, want true")
	}
}

func TestToolConfigForConfigAddsContext7WhenAPIKeyIsSet(t *testing.T) {
	base := mcp.Config{Servers: map[string]mcp.ServerConfig{
		"obscura": {Transport: mcp.TransportStreamableHTTP, URL: "http://obscura:8090/mcp"},
	}}
	cfg := config.Config{
		Context7APIKey: "ctx-key",
		Context7MCPURL: "https://mcp.context7.com/mcp",
	}

	got, collision := toolConfigForConfig(cfg, base)
	if collision {
		t.Fatal("toolConfigForConfig() collision = true, want false")
	}
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

func TestToolConfigForConfigPreservesExternalContext7OnCollision(t *testing.T) {
	base := mcp.Config{Servers: map[string]mcp.ServerConfig{
		"context7": {Transport: mcp.TransportStreamableHTTP, URL: "http://custom-context7:8080/mcp"},
	}}
	cfg := config.Config{
		Context7APIKey: "ctx-key",
		Context7MCPURL: "https://mcp.context7.com/mcp",
	}

	got, collision := toolConfigForConfig(cfg, base)
	if !collision {
		t.Fatal("toolConfigForConfig() collision = false, want true")
	}
	if got.Servers["context7"].URL != "http://custom-context7:8080/mcp" {
		t.Fatalf("context7 config = %#v, want external config preserved", got.Servers["context7"])
	}
}

func TestContext7ConfiguredDetectsEnvOrExternalServer(t *testing.T) {
	if context7Configured(config.Config{}, mcp.Config{}) {
		t.Fatal("context7Configured() = true without API key or external server, want false")
	}
	if !context7Configured(config.Config{Context7APIKey: "ctx-key"}, mcp.Config{}) {
		t.Fatal("context7Configured() = false with API key, want true")
	}
	if !context7Configured(config.Config{}, mcp.Config{Servers: map[string]mcp.ServerConfig{
		"context7": {Transport: mcp.TransportStreamableHTTP, URL: "http://custom-context7:8080/mcp"},
	}}) {
		t.Fatal("context7Configured() = false with external context7 server, want true")
	}
}

func TestToolConfigForConfigPreservesExternalTavilyOnCollision(t *testing.T) {
	base := mcp.Config{Servers: map[string]mcp.ServerConfig{
		"tavily": {Transport: mcp.TransportStreamableHTTP, URL: "http://custom-tavily-mcp:8080/mcp"},
	}}
	cfg := config.Config{TavilyURL: "https://mcp.tavily.com/mcp/", TavilyAPIKey: "secret"}

	got, collision := toolConfigForConfig(cfg, base)
	if !collision {
		t.Fatal("toolConfigForConfig() collision = false, want true")
	}
	if got.Servers["tavily"].URL != "http://custom-tavily-mcp:8080/mcp" {
		t.Fatalf("tavily config = %#v, want external config preserved", got.Servers["tavily"])
	}
}
