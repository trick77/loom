package main

import (
	"testing"

	"github.com/trick77/spark/internal/config"
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
