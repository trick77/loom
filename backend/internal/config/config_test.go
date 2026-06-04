package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_defaults(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr default = %q, want :8080", cfg.Addr)
	}
	if cfg.DBPath != "/data/spark.db" {
		t.Errorf("DBPath default = %q, want /data/spark.db", cfg.DBPath)
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
}

func TestLoad_overrides_and_required(t *testing.T) {
	t.Setenv("SPARK_ADDR", ":9000")
	t.Setenv("SPARK_SESSION_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when SPARK_SESSION_SECRET is empty")
	}
}

func TestLoad_chatReasoningEffort(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")
	t.Setenv("SPARK_CHAT_REASONING_EFFORT", "low")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ChatReasoningEffort != "low" {
		t.Fatalf("ChatReasoningEffort = %q, want low", cfg.ChatReasoningEffort)
	}
}

func TestLoad_context7Settings(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")
	t.Setenv("SPARK_CONTEXT7_API_KEY", "ctx-key")
	t.Setenv("SPARK_CONTEXT7_MCP_URL", "https://context7.example/mcp")

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
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")
	t.Setenv("SPARK_CONTEXT7_API_KEY", "ctx-key")
	t.Setenv("SPARK_CONTEXT7_MCP_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when Context7 API key is set without MCP URL")
	}
}

func TestLoadImageGenerationDefaultsDisabled(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "secret")
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
	if cfg.BFLPollTimeout != 3*time.Minute {
		t.Fatalf("BFLPollTimeout = %s, want 3m0s", cfg.BFLPollTimeout)
	}
}

func TestLoadBFLImageRequiresBaseURLWhenAPIKeyIsSet(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "secret")
	t.Setenv("SPARK_BFL_API_KEY", "bfl-test")
	t.Setenv("SPARK_BFL_BASE_URL", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "SPARK_BFL_BASE_URL is required") {
		t.Fatalf("Load() error = %v, want SPARK_BFL_BASE_URL required", err)
	}
}

func TestLoadBFLImageConfiguredByAPIKey(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "secret")
	t.Setenv("SPARK_BFL_API_KEY", "bfl-test")
	t.Setenv("SPARK_BFL_MODEL", "flux-2-klein-9b")
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
	t.Setenv("SPARK_SESSION_SECRET", "secret")
	t.Setenv("SPARK_BFL_POLL_TIMEOUT", "7m")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.BFLPollTimeout != 7*time.Minute {
		t.Fatalf("BFLPollTimeout = %s, want 7m0s", cfg.BFLPollTimeout)
	}
}

func TestLoadBFLImageRejectsInvalidPollTimeout(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "secret")
	t.Setenv("SPARK_BFL_POLL_TIMEOUT", "soon")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "SPARK_BFL_POLL_TIMEOUT must be a duration") {
		t.Fatalf("Load() error = %v, want invalid poll timeout", err)
	}
}

func TestLoad_defaultsDoNotRequireAdminPassword(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.AdminInitialPassword != "" {
		t.Fatalf("AdminInitialPassword = %q, want empty legacy field", cfg.AdminInitialPassword)
	}
}

func TestLoad_oidcSettings(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")
	t.Setenv("SPARK_PUBLIC_URL", "https://spark.example.com")
	t.Setenv("SPARK_OIDC_ISSUER", "https://auth.example.com/application/o/spark/")
	t.Setenv("SPARK_OIDC_CLIENT_ID", "spark-client")
	t.Setenv("SPARK_OIDC_CLIENT_SECRET", "spark-secret")
	t.Setenv("SPARK_OIDC_REDIRECT_URL", "https://spark.example.com/api/auth/callback")
	t.Setenv("SPARK_OIDC_POST_LOGOUT_REDIRECT_URL", "https://spark.example.com/")
	t.Setenv("SPARK_OIDC_ADMIN_GROUP", "spark-admins")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.OIDC.Issuer != "https://auth.example.com/application/o/spark/" {
		t.Fatalf("OIDC issuer = %q", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.AdminGroup != "spark-admins" {
		t.Fatalf("OIDC admin group = %q", cfg.OIDC.AdminGroup)
	}
}

func TestLoad_oidcSettingsMustBeComplete(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")
	t.Setenv("SPARK_OIDC_ISSUER", "https://auth.example.com/application/o/spark/")
	t.Setenv("SPARK_OIDC_CLIENT_ID", "spark-client")
	t.Setenv("SPARK_OIDC_REDIRECT_URL", "https://spark.example.com/api/auth/callback")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when OIDC issuer is set without client secret")
	}
}

func TestLoad_devAuthRequiresLoopbackAddr(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")
	t.Setenv("SPARK_AUTH_MODE", "dev")
	t.Setenv("SPARK_ADDR", ":8080")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when dev auth listens on all interfaces")
	}
}

func TestLoad_devAuthRejectsPublicNonLoopbackURL(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")
	t.Setenv("SPARK_AUTH_MODE", "dev")
	t.Setenv("SPARK_ADDR", "localhost:8080")
	t.Setenv("SPARK_PUBLIC_URL", "https://spark.example.com")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when dev auth has a non-loopback public URL")
	}
}

func TestLoad_devAuthAllowsLoopbackAdmin(t *testing.T) {
	t.Setenv("SPARK_SESSION_SECRET", "test-secret")
	t.Setenv("SPARK_AUTH_MODE", "dev")
	t.Setenv("SPARK_ADDR", "127.0.0.1:8080")
	t.Setenv("SPARK_PUBLIC_URL", "http://localhost:8080")

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
