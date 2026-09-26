package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/trick77/llmwire"
	"github.com/trick77/loom/internal/rag"
)

func TestLoad_defaults(t *testing.T) {
	requiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr default = %q, want :8080", cfg.Addr)
	}
	if cfg.DBPath != "/data/loom.db" {
		t.Errorf("DBPath default = %q, want /data/loom.db", cfg.DBPath)
	}
	if cfg.UsersDir != "/data/users" {
		t.Errorf("UsersDir default = %q, want /data/users", cfg.UsersDir)
	}
	if cfg.ChatLogDir != "logs/llm-responses" {
		t.Errorf("ChatLogDir default = %q, want logs/llm-responses", cfg.ChatLogDir)
	}
	if cfg.ChatMaxCompletionTokens != 16384 {
		t.Errorf("ChatMaxCompletionTokens default = %d, want 16384", cfg.ChatMaxCompletionTokens)
	}
	if cfg.ChatTimeout != 4*time.Minute {
		t.Errorf("ChatTimeout default = %s, want 4m0s", cfg.ChatTimeout)
	}
	if cfg.ChatIdleTimeout != 120*time.Second {
		t.Errorf("ChatIdleTimeout default = %s, want 2m0s", cfg.ChatIdleTimeout)
	}
	if cfg.TavilyURL != "https://mcp.tavily.com/mcp/" {
		t.Errorf("TavilyURL default = %q, want https://mcp.tavily.com/mcp/", cfg.TavilyURL)
	}
	if cfg.TavilyAPIKey != "" {
		t.Errorf("TavilyAPIKey default = %q, want empty opt-in value", cfg.TavilyAPIKey)
	}
	if cfg.ObscuraMCPURL != "" {
		t.Errorf("ObscuraMCPURL default = %q, want empty opt-in value", cfg.ObscuraMCPURL)
	}
	if cfg.GotenbergURL != "http://gotenberg:3000" {
		t.Errorf("GotenbergURL default = %q, want http://gotenberg:3000", cfg.GotenbergURL)
	}
}

func TestLoad_gotenbergURLOverride(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_GOTENBERG_URL", "http://localhost:3000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.GotenbergURL != "http://localhost:3000" {
		t.Errorf("GotenbergURL = %q, want override", cfg.GotenbergURL)
	}
}

func TestLoad_overrides_and_required(t *testing.T) {
	t.Setenv("BACKEND_ADDR", ":9000")
	t.Setenv("BACKEND_SESSION_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when BACKEND_SESSION_SECRET is empty")
	}
}

func TestLoad_chatGenerationBounds(t *testing.T) {
	requiredEnv(t)
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
			requiredEnv(t)
			t.Setenv(tc.key, tc.value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoad_firstClassMCPToolURLs(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_OBSCURA_MCP_URL", "http://obscura:8090/mcp")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ObscuraMCPURL != "http://obscura:8090/mcp" {
		t.Fatalf("ObscuraMCPURL = %q, want obscura MCP URL", cfg.ObscuraMCPURL)
	}
}

func TestLoadImageGenerationDefaultsDisabled(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ImageGenAPIKey != "" {
		t.Fatal("ImageGenAPIKey default was not empty")
	}
	if cfg.ImageGenBaseURL != "https://queue.fal.run" {
		t.Fatalf("ImageGenBaseURL = %q", cfg.ImageGenBaseURL)
	}
	if cfg.ImageGenModel != "fal-ai/flux-2-pro" {
		t.Fatalf("ImageGenModel = %q", cfg.ImageGenModel)
	}
	if cfg.ImageGenTypographyModel != "fal-ai/flux-2-max" {
		t.Fatalf("ImageGenTypographyModel = %q", cfg.ImageGenTypographyModel)
	}
	if cfg.ImageGenPollTimeout != 1*time.Minute {
		t.Fatalf("ImageGenPollTimeout = %s, want 1m0s", cfg.ImageGenPollTimeout)
	}
}

func TestLoadImageGenRequiresBaseURLWhenAPIKeyIsSet(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_IMAGE_GEN_API_KEY", "fal-test")
	t.Setenv("BACKEND_IMAGE_GEN_BASE_URL", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "BACKEND_IMAGE_GEN_BASE_URL must be an absolute") {
		t.Fatalf("Load() error = %v, want BACKEND_IMAGE_GEN_BASE_URL required", err)
	}
}

func TestLoadImageGenConfiguredByAPIKey(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_IMAGE_GEN_API_KEY", "fal-test")
	t.Setenv("BACKEND_IMAGE_GEN_MODEL", "flux-2-klein-9b")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ImageGenAPIKey != "fal-test" {
		t.Fatalf("ImageGenAPIKey was not loaded")
	}
	if cfg.ImageGenModel != "flux-2-klein-9b" {
		t.Fatalf("ImageGenModel = %q", cfg.ImageGenModel)
	}
}

func TestLoadImageGenPollTimeoutOverride(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_IMAGE_GEN_POLL_TIMEOUT", "7m")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ImageGenPollTimeout != 7*time.Minute {
		t.Fatalf("ImageGenPollTimeout = %s, want 7m0s", cfg.ImageGenPollTimeout)
	}
}

func TestLoadImageGenRejectsInvalidPollTimeout(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_IMAGE_GEN_POLL_TIMEOUT", "soon")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "BACKEND_IMAGE_GEN_POLL_TIMEOUT must be a duration") {
		t.Fatalf("Load() error = %v, want invalid poll timeout", err)
	}
}

func TestLoad_oidcSettings(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_PUBLIC_URL", "https://loom.example.com")
	t.Setenv("BACKEND_OIDC_ISSUER", "https://auth.example.com/application/o/loom/")
	t.Setenv("BACKEND_OIDC_CLIENT_ID", "loom-client")
	t.Setenv("BACKEND_OIDC_CLIENT_SECRET", "loom-secret")
	t.Setenv("BACKEND_OIDC_REDIRECT_URL", "https://loom.example.com/api/auth/callback")
	t.Setenv("BACKEND_OIDC_POST_LOGOUT_REDIRECT_URL", "https://loom.example.com/")
	t.Setenv("BACKEND_OIDC_ADMIN_GROUP", "loom-admins")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.OIDC.Issuer != "https://auth.example.com/application/o/loom/" {
		t.Fatalf("OIDC issuer = %q", cfg.OIDC.Issuer)
	}
	if cfg.OIDC.AdminGroup != "loom-admins" {
		t.Fatalf("OIDC admin group = %q", cfg.OIDC.AdminGroup)
	}
}

func TestLoad_oidcSettingsMustBeComplete(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_OIDC_ISSUER", "https://auth.example.com/application/o/loom/")
	t.Setenv("BACKEND_OIDC_CLIENT_ID", "loom-client")
	t.Setenv("BACKEND_OIDC_REDIRECT_URL", "https://loom.example.com/api/auth/callback")
	t.Setenv("BACKEND_OIDC_CLIENT_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when OIDC issuer is set without client secret")
	}
}

func TestLoad_devAuthRequiresLoopbackAddr(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_AUTH_MODE", "dev")
	t.Setenv("BACKEND_ADDR", ":8080")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when dev auth listens on all interfaces")
	}
}

func TestLoad_devAuthRejectsPublicNonLoopbackURL(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_AUTH_MODE", "dev")
	t.Setenv("BACKEND_ADDR", "localhost:8080")
	t.Setenv("BACKEND_PUBLIC_URL", "https://loom.example.com")

	if _, err := Load(); err == nil {
		t.Fatal("expected error when dev auth has a non-loopback public URL")
	}
}

func TestLoad_devAuthAllowsLoopbackAdmin(t *testing.T) {
	requiredEnv(t)
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

// anyChatModel is a registry chat model loom's chat role accepts, found at run
// time so no test names a model.
func anyChatModel(t *testing.T) (id, keyEnv string) {
	t.Helper()
	reg := llmwire.Default()
	ids := reg.ChatModels(llmwire.Needs{Tools: true, Streaming: true, Vision: true})
	if len(ids) == 0 {
		t.Skip("llmwire's registry has no chat model with tools and vision")
	}
	p, err := reg.Lookup(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	return ids[0], p.APIKeyEnv()
}

// anyEmbedModel is a registry embedding model, found at run time so no test
// names a model.
func anyEmbedModel(t *testing.T) rag.EmbedModel {
	t.Helper()
	reg := llmwire.Default()
	for _, id := range reg.Models() {
		if m, err := rag.ResolveEmbedModel(reg, id); err == nil {
			return m
		}
	}
	t.Skip("llmwire's registry has no embedding model")
	return rag.EmbedModel{}
}

// Outside dev auth a deployment without a chat model cannot answer a turn, so
// it must not boot as if it could (an upgrade that only set the key did that).
// Dev auth boots without one: a UI session needs no model.
func TestLoad_chatModelRequiredOutsideDevAuth(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_CHAT_MODEL", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "BACKEND_CHAT_MODEL") {
		t.Fatalf("oidc without a chat model: err = %v, want one naming BACKEND_CHAT_MODEL", err)
	}
	t.Setenv("BACKEND_AUTH_MODE", "dev")
	t.Setenv("BACKEND_ADDR", "127.0.0.1:8080")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("dev without a chat model: %v", err)
	}
	if cfg.ChatEnabled || cfg.ChatMissing != "BACKEND_CHAT_MODEL" {
		t.Fatalf("dev: enabled=%v missing=%q, want off, BACKEND_CHAT_MODEL", cfg.ChatEnabled, cfg.ChatMissing)
	}
}

// Chat is on when a chat model is configured and every key its roles' providers
// read is set.
func TestLoad_modelCapabilitiesFollowModelsAndKeys(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_AUTH_MODE", "dev")
	t.Setenv("BACKEND_ADDR", "127.0.0.1:8080")
	model, keyEnv := anyChatModel(t)
	t.Setenv("BACKEND_CHAT_MODEL", "")
	t.Setenv(keyEnv, "k1")
	t.Setenv("BACKEND_EMBED_MODEL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatEnabled || cfg.ChatMissing != "BACKEND_CHAT_MODEL" {
		t.Fatalf("no chat model: enabled=%v missing=%q, want off, BACKEND_CHAT_MODEL", cfg.ChatEnabled, cfg.ChatMissing)
	}

	t.Setenv("BACKEND_CHAT_MODEL", model)
	t.Setenv(keyEnv, "")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatEnabled || cfg.ChatMissing != keyEnv {
		t.Fatalf("no key: enabled=%v missing=%q, want off, %s", cfg.ChatEnabled, cfg.ChatMissing, keyEnv)
	}

	t.Setenv(keyEnv, "k1")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ChatEnabled || cfg.EmbedEnabled {
		t.Fatalf("chat only: chat=%v embed=%v", cfg.ChatEnabled, cfg.EmbedEnabled)
	}
	if cfg.ChatModels.Info().ID != model {
		t.Fatalf("resolved chat model = %q, want %q", cfg.ChatModels.Info().ID, model)
	}
}

// Embeddings are on when an embedding model is configured and its key is set;
// an id that is not an embedding model fails boot with the valid choices.
func TestLoad_embeddingsFollowTheModelAndItsKey(t *testing.T) {
	requiredEnv(t)
	m := anyEmbedModel(t)
	t.Setenv("BACKEND_EMBED_MODEL", "")
	t.Setenv(m.KeyEnv, "k1")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EmbedEnabled || cfg.EmbedMissing != "BACKEND_EMBED_MODEL" {
		t.Fatalf("no model: enabled=%v missing=%q", cfg.EmbedEnabled, cfg.EmbedMissing)
	}
	t.Setenv("BACKEND_EMBED_MODEL", m.ID)
	t.Setenv(m.KeyEnv, " ")
	if cfg, err = Load(); err != nil || cfg.EmbedEnabled || cfg.EmbedMissing != m.KeyEnv {
		t.Fatalf("no key: enabled=%v missing=%q err=%v, want off, %s", cfg.EmbedEnabled, cfg.EmbedMissing, err, m.KeyEnv)
	}
	t.Setenv(m.KeyEnv, "k1")
	if cfg, err = Load(); err != nil || !cfg.EmbedEnabled || cfg.EmbedModel != m {
		t.Fatalf("model and key: enabled=%v model=%+v err=%v, want on, %+v", cfg.EmbedEnabled, cfg.EmbedModel, err, m)
	}
	t.Setenv("BACKEND_EMBED_MODEL", "no-such-model")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "valid choices") {
		t.Fatalf("unknown model: err = %v, want one listing the valid choices", err)
	}
}

// A model id llmwire does not know, or one short of its role, fails boot.
func TestLoad_unusableChatModelFailsBoot(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_CHAT_MODEL", "no-such-model")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "valid choices") {
		t.Fatalf("Load() error = %v, want one listing the valid choices", err)
	}
}

// requiredEnv sets the smallest environment Load accepts: the session secret
// and a complete OIDC setup. Tests override individual keys after calling it.
func requiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("BACKEND_SESSION_SECRET", "test-secret")
	t.Setenv("BACKEND_AUTH_MODE", "oidc")
	t.Setenv("BACKEND_OIDC_ISSUER", "https://idp.example.com")
	t.Setenv("BACKEND_OIDC_CLIENT_ID", "loom")
	t.Setenv("BACKEND_OIDC_CLIENT_SECRET", "s3cret")
	t.Setenv("BACKEND_OIDC_REDIRECT_URL", "https://loom.example.com/api/auth/callback")
	t.Setenv("BACKEND_OIDC_ADMIN_GROUP", "loom-admins")
	model, _ := anyChatModel(t)
	t.Setenv("BACKEND_CHAT_MODEL", model)
}

// An unset auth mode with no issuer booted a server nobody could log in to.
func TestLoad_rejectsEmptyAuthMode(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_AUTH_MODE", "")
	t.Setenv("BACKEND_OIDC_ISSUER", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "BACKEND_AUTH_MODE") {
		t.Fatalf("Load() error = %v, want an auth mode error", err)
	}
}

func TestLoad_validatesURLsAndDirs(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, want string
		extra                  map[string]string
	}{
		{"relative public url", "BACKEND_PUBLIC_URL", "loom.example.com", "BACKEND_PUBLIC_URL", nil},
		{"public url without host", "BACKEND_PUBLIC_URL", "https://", "BACKEND_PUBLIC_URL", nil},
		{"relative users dir", "BACKEND_USERS_DIR", "data/users", "BACKEND_USERS_DIR", nil},
		{"empty db path", "BACKEND_DB_PATH", "", "BACKEND_DB_PATH", nil},
		{"image gen base url without scheme", "BACKEND_IMAGE_GEN_BASE_URL", "queue.fal.run", "BACKEND_IMAGE_GEN_BASE_URL", map[string]string{"BACKEND_IMAGE_GEN_API_KEY": "k"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requiredEnv(t)
			for k, v := range tc.extra {
				t.Setenv(k, v)
			}
			t.Setenv(tc.key, tc.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() error = %v, want it to name %s", err, tc.want)
			}
		})
	}
}

// An empty admin group is a legal but surprising setup (nobody is admin), so
// it is logged rather than refused.
func TestLoad_warnsOnEmptyAdminGroup(t *testing.T) {
	requiredEnv(t)
	t.Setenv("BACKEND_OIDC_ADMIN_GROUP", "")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if !strings.Contains(logs.String(), "BACKEND_OIDC_ADMIN_GROUP") {
		t.Fatalf("no warning about the empty admin group:\n%s", logs.String())
	}
}

func TestLoad_sessionTTL(t *testing.T) {
	requiredEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.SessionTTL != 30*24*time.Hour {
		t.Fatalf("SessionTTL default = %s, want 720h", cfg.SessionTTL)
	}

	t.Setenv("BACKEND_SESSION_TTL", "12h")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.SessionTTL != 12*time.Hour {
		t.Fatalf("SessionTTL = %s, want 12h", cfg.SessionTTL)
	}

	t.Setenv("BACKEND_SESSION_TTL", "0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "BACKEND_SESSION_TTL") {
		t.Fatalf("Load() error = %v, want a session TTL error", err)
	}
}
