package main

import (
	"testing"

	"github.com/trick77/spark/internal/config"
	"github.com/trick77/spark/internal/mcp"
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

func TestToolConfigForConfigAddsBuiltInSearxng(t *testing.T) {
	base := mcp.Config{Servers: map[string]mcp.ServerConfig{
		"obscura": {Transport: mcp.TransportStreamableHTTP, URL: "http://obscura:8090/mcp"},
	}}
	cfg := config.Config{SearxngURL: "http://searxng:8080"}

	got, collision := toolConfigForConfig(cfg, base)
	if collision {
		t.Fatal("toolConfigForConfig() collision = true, want false")
	}
	if got.Servers["obscura"].URL != "http://obscura:8090/mcp" {
		t.Fatalf("obscura config = %#v", got.Servers["obscura"])
	}
	searxng := got.Servers["searxng"]
	if searxng.URL != "http://searxng:8080" {
		t.Fatalf("searxng URL = %q, want configured URL", searxng.URL)
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

func TestToolConfigForConfigLeavesSearxngDisabledWhenURLIsEmpty(t *testing.T) {
	got, collision := toolConfigForConfig(config.Config{}, mcp.Config{})
	if collision {
		t.Fatal("toolConfigForConfig() collision = true, want false")
	}
	if _, exists := got.Servers["searxng"]; exists {
		t.Fatalf("searxng server exists when SPARK_SEARXNG_URL is empty: %#v", got.Servers["searxng"])
	}
}

func TestToolConfigForConfigPreservesExternalSearxngOnCollision(t *testing.T) {
	base := mcp.Config{Servers: map[string]mcp.ServerConfig{
		"searxng": {Transport: mcp.TransportStreamableHTTP, URL: "http://custom-searxng-mcp:8080/mcp"},
	}}
	cfg := config.Config{SearxngURL: "http://searxng:8080"}

	got, collision := toolConfigForConfig(cfg, base)
	if !collision {
		t.Fatal("toolConfigForConfig() collision = false, want true")
	}
	if got.Servers["searxng"].URL != "http://custom-searxng-mcp:8080/mcp" {
		t.Fatalf("searxng config = %#v, want external config preserved", got.Servers["searxng"])
	}
}
