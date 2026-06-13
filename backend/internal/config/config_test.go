package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_defaults(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr default = %q, want :8080", cfg.Addr)
	}
	if cfg.DBPath != "/data/slopr.db" {
		t.Errorf("DBPath default = %q, want /data/slopr.db", cfg.DBPath)
	}
	if cfg.UsersDir != "/data/users" {
		t.Errorf("UsersDir default = %q, want /data/users", cfg.UsersDir)
	}
	if cfg.ChatLogDir != "logs/llm-responses" {
		t.Errorf("ChatLogDir default = %q, want logs/llm-responses", cfg.ChatLogDir)
	}
	if cfg.ChatReasoningEffort != "high" {
		t.Errorf("ChatReasoningEffort default = %q, want high", cfg.ChatReasoningEffort)
	}
	if cfg.ChatMaxCompletionTokens != 2048 {
		t.Errorf("ChatMaxCompletionTokens default = %d, want 2048", cfg.ChatMaxCompletionTokens)
	}
	if cfg.ChatTimeout != 2*time.Minute {
		t.Errorf("ChatTimeout default = %s, want 2m0s", cfg.ChatTimeout)
	}
	if cfg.ChatIdleTimeout != 60*time.Second {
		t.Errorf("ChatIdleTimeout default = %s, want 60s", cfg.ChatIdleTimeout)
	}
	if cfg.TavilyURL != "https://mcp.tavily.com/mcp/" {
		t.Errorf("TavilyURL default = %q, want https://mcp.tavily.com/mcp/", cfg.TavilyURL)
	}
	if cfg.TavilyAPIKey != "" {
		t.Errorf("TavilyAPIKey default = %q, want empty opt-in value", cfg.TavilyAPIKey)
	}
	if cfg.Context7MCPURL != "https://mcp.context7.com/mcp" {
		t.Errorf("Context7MCPURL default = %q, want Context7 remote endpoint", cfg.Context7MCPURL)
	}
	if cfg.Context7APIKey != "" {
		t.Errorf("Context7APIKey default = %q, want empty opt-in value", cfg.Context7APIKey)
	}
	if cfg.FetchMCPURL != "" {
		t.Errorf("FetchMCPURL default = %q, want empty opt-in value", cfg.FetchMCPURL)
	}
	if cfg.ObscuraMCPURL != "" {
		t.Errorf("ObscuraMCPURL default = %q, want empty opt-in value", cfg.ObscuraMCPURL)
	}
}

func TestLoad_overrides_and_required(t *testing.T) {
	t.Setenv("BACKEND_ADDR", ":9000")
	t.Setenv("BACKEND_SESSION_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when BACKEND_SESSION_SECRET is empty")
	}
}

func TestLoad_chatReasoningEffort(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_CHAT_REASONING_EFFORT", "low")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ChatReasoningEffort != "low" {
		t.Fatalf("ChatReasoningEffort = %q, want low", cfg.ChatReasoningEffort)
	}
}

func TestLoad_chatGenerationBounds(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_CHAT_MAX_COMPLETION_TOKENS", "4096")
	t.Setenv("BACKEND_CHAT_TIMEOUT", "45s")
	t.Setenv("BACKEND_CHAT_IDLE_TIMEOUT", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ChatMaxCompletionTokens != 4096 {
		t.Fatalf("ChatMaxCompletionTokens = %d, want 4096", cfg.ChatMaxCompletionTokens)
	}
	if cfg.ChatTimeout != 45*time.Second {
		t.Fatalf("ChatTimeout = %s, want 45s", cfg.ChatTimeout)
	}
	if cfg.ChatIdleTimeout != 0 {
		t.Fatalf("ChatIdleTimeout = %s, want 0 (watchdog disabled)", cfg.ChatIdleTimeout)
	}
}

func TestLoad_rejectsInvalidChatGenerationBounds(t *testing.T) {
	for _, tc := range []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{
			name:    "non-integer max completion tokens",
			key:     "BACKEND_CHAT_MAX_COMPLETION_TOKENS",
			value:   "many",
			wantErr: "BACKEND_CHAT_MAX_COMPLETION_TOKENS must be an integer greater than 0",
		},
		{
			name:    "zero max completion tokens",
			key:     "BACKEND_CHAT_MAX_COMPLETION_TOKENS",
			value:   "0",
			wantErr: "BACKEND_CHAT_MAX_COMPLETION_TOKENS must be an integer greater than 0",
		},
		{
			name:    "invalid timeout",
			key:     "BACKEND_CHAT_TIMEOUT",
			value:   "soon",
			wantErr: "BACKEND_CHAT_TIMEOUT must be a duration greater than 0",
		},
		{
			name:    "zero timeout",
			key:     "BACKEND_CHAT_TIMEOUT",
			value:   "0s",
			wantErr: "BACKEND_CHAT_TIMEOUT must be a duration greater than 0",
		},
		{
			name:    "invalid idle timeout",
			key:     "BACKEND_CHAT_IDLE_TIMEOUT",
			value:   "soon",
			wantErr: "BACKEND_CHAT_IDLE_TIMEOUT must be a non-negative duration (0 disables the idle watchdog)",
		},
		{
			name:    "negative idle timeout",
			key:     "BACKEND_CHAT_IDLE_TIMEOUT",
			value:   "-5s",
			wantErr: "BACKEND_CHAT_IDLE_TIMEOUT must be a non-negative duration (0 disables the idle watchdog)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
			t.Setenv(tc.key, tc.value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoad_context7Settings(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_CONTEXT7_API_KEY", "ctx-key")
	t.Setenv("BACKEND_CONTEXT7_MCP_URL", "https://context7.example/mcp")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Context7APIKey != "ctx-key" {
		t.Fatalf("Context7APIKey = %q, want ctx-key", cfg.Context7APIKey)
	}
	if cfg.Context7MCPURL != "https://context7.example/mcp" {
		t.Fatalf("Context7MCPURL = %q, want override", cfg.Context7MCPURL)
	}
}

func TestLoad_context7RequiresURLWhenEnabled(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_CONTEXT7_API_KEY", "ctx-key")
	t.Setenv("BACKEND_CONTEXT7_MCP_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when Context7 API key is set without MCP URL")
	}
}

func TestLoad_firstClassMCPToolURLs(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_FETCH_MCP_URL", "http://fetch:8090/mcp")
	t.Setenv("BACKEND_OBSCURA_MCP_URL", "http://obscura:8090/mcp")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.FetchMCPURL != "http://fetch:8090/mcp" {
		t.Fatalf("FetchMCPURL = %q, want fetch MCP URL", cfg.FetchMCPURL)
	}
	if cfg.ObscuraMCPURL != "http://obscura:8090/mcp" {
		t.Fatalf("ObscuraMCPURL = %q, want obscura MCP URL", cfg.ObscuraMCPURL)
	}
}

func TestLoadImageGenerationDefaultsDisabled(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "secret")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BFLAPIKey != "" {
		t.Fatal("BFLAPIKey default was not empty")
	}
	if cfg.BFLBaseURL != "https://api.bfl.ai/v1" {
		t.Fatalf("BFLBaseURL = %q", cfg.BFLBaseURL)
	}
	if cfg.BFLModel != "flux-2-klein-4b" {
		t.Fatalf("BFLModel = %q", cfg.BFLModel)
	}
	if cfg.BFLPollTimeout != 1*time.Minute {
		t.Fatalf("BFLPollTimeout = %s, want 1m0s", cfg.BFLPollTimeout)
	}
}

func TestLoadBFLImageRequiresBaseURLWhenAPIKeyIsSet(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "secret")
	t.Setenv("BACKEND_BFL_API_KEY", "bfl-test")
	t.Setenv("BACKEND_BFL_BASE_URL", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "BACKEND_BFL_BASE_URL is required") {
		t.Fatalf("Load() error = %v, want BACKEND_BFL_BASE_URL required", err)
	}
}

func TestLoadBFLImageConfiguredByAPIKey(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "secret")
	t.Setenv("BACKEND_BFL_API_KEY", "bfl-test")
	t.Setenv("BACKEND_BFL_MODEL", "flux-2-klein-9b")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BFLAPIKey != "bfl-test" {
		t.Fatalf("BFLAPIKey was not loaded")
	}
	if cfg.BFLModel != "flux-2-klein-9b" {
		t.Fatalf("BFLModel = %q", cfg.BFLModel)
	}
}

func TestLoadBFLImagePollTimeoutOverride(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "secret")
	t.Setenv("BACKEND_BFL_POLL_TIMEOUT", "7m")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BFLPollTimeout != 7*time.Minute {
		t.Fatalf("BFLPollTimeout = %s, want 7m0s", cfg.BFLPollTimeout)
	}
}

func TestLoadBFLImageRejectsInvalidPollTimeout(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "secret")
	t.Setenv("BACKEND_BFL_POLL_TIMEOUT", "soon")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "BACKEND_BFL_POLL_TIMEOUT must be a duration") {
		t.Fatalf("Load() error = %v, want invalid poll timeout", err)
	}
}

func TestLoad_defaultsDoNotRequireAdminPassword(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.AdminInitialPassword != "" {
		t.Fatalf("AdminInitialPassword = %q, want empty legacy field", cfg.AdminInitialPassword)
	}
}

func TestLoad_oidcSettings(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_PUBLIC_URL", "https://slopr.example.com")
	t.Setenv("BACKEND_OIDC_ISSUER", "https://auth.example.com/application/o/slopr/")
	t.Setenv("BACKEND_OIDC_CLIENT_ID", "slopr-client")
	t.Setenv("BACKEND_OIDC_CLIENT_SECRET", "slopr-secret")
	t.Setenv("BACKEND_OIDC_REDIRECT_URL", "https://slopr.example.com/api/auth/callback")
	t.Setenv("BACKEND_OIDC_POST_LOGOUT_REDIRECT_URL", "https://slopr.example.com/")
	t.Setenv("BACKEND_OIDC_ADMIN_GROUP", "slopr-admins")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.OIDC.Issuer != "https://auth.example.com/application/o/slopr/" {
		t.Fatalf("OIDC issuer = %q", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.AdminGroup != "slopr-admins" {
		t.Fatalf("OIDC admin group = %q", cfg.OIDC.AdminGroup)
	}
}

func TestLoad_oidcSettingsMustBeComplete(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_OIDC_ISSUER", "https://auth.example.com/application/o/slopr/")
	t.Setenv("BACKEND_OIDC_CLIENT_ID", "slopr-client")
	t.Setenv("BACKEND_OIDC_REDIRECT_URL", "https://slopr.example.com/api/auth/callback")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when OIDC issuer is set without client secret")
	}
}

func TestLoad_devAuthRequiresLoopbackAddr(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_AUTH_MODE", "dev")
	t.Setenv("BACKEND_ADDR", ":8080")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when dev auth listens on all interfaces")
	}
}

func TestLoad_devAuthRejectsPublicNonLoopbackURL(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_AUTH_MODE", "dev")
	t.Setenv("BACKEND_ADDR", "localhost:8080")
	t.Setenv("BACKEND_PUBLIC_URL", "https://slopr.example.com")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when dev auth has a non-loopback public URL")
	}
}

func TestLoad_devAuthAllowsLoopbackAdmin(t *testing.T) {
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_AUTH_MODE", "dev")
	t.Setenv("BACKEND_ADDR", "127.0.0.1:8080")
	t.Setenv("BACKEND_PUBLIC_URL", "http://localhost:8080")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.AuthMode != AuthModeDev {
		t.Fatalf("AuthMode = %q, want dev", cfg.AuthMode)
	}
	if cfg.DevUser.Role != "admin" {
		t.Fatalf("DevUser role = %q, want admin", cfg.DevUser.Role)
	}
}
