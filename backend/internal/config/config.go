// Package config loads slopr's runtime configuration from environment variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Keep in sync with imagegen's direct-client fallback default.
const defaultBFLPollTimeout = 1 * time.Minute
const defaultChatMaxCompletionTokens = 2048
const defaultChatTimeout = 2 * time.Minute

// AuthMode selects how Slopr signs users in.
type AuthMode string

const (
	AuthModeNone AuthMode = ""
	AuthModeOIDC AuthMode = "oidc"
	AuthModeDev  AuthMode = "dev"
)

// Config holds all runtime settings. Secrets come from ENV only.
type Config struct {
	Addr      string // HTTP listen address
	DBPath    string // path to the SQLite file
	UsersDir  string // root for per-user volumes: <UsersDir>/<user-id>/
	PublicURL string // externally reachable base URL

	ChatBaseURL             string // OpenAI-compatible chat endpoint (MiMo)
	ChatAPIKey              string
	ChatModel               string
	ChatReasoningEffort     string
	ChatMaxCompletionTokens int
	ChatTimeout             time.Duration
	ChatLogDir              string
	EmbedBaseURL            string // OpenAI embeddings endpoint
	EmbedAPIKey             string
	EmbedModel              string
	BFLBaseURL              string
	BFLAPIKey               string
	BFLModel                string
	BFLPollTimeout          time.Duration

	TikaURL        string
	TavilyURL      string // hosted Tavily MCP endpoint for built-in web search
	TavilyAPIKey   string // enables built-in Tavily web search when set
	FetchMCPURL    string
	ObscuraMCPURL  string
	Context7MCPURL string
	Context7APIKey string

	AdminInitialPassword string // legacy; authentik owns credentials in Phase 2
	SessionSecret        string
	AuthMode             AuthMode
	OIDC                 OIDCConfig
	DevUser              DevUserConfig
}

// OIDCConfig holds authentik OpenID Connect settings.
type OIDCConfig struct {
	Issuer                string
	ClientID              string
	ClientSecret          string
	RedirectURL           string
	PostLogoutRedirectURL string
	AdminGroup            string
}

// DevUserConfig holds the fixed local-only development identity.
type DevUserConfig struct {
	Subject     string
	Username    string
	Email       string
	DisplayName string
	Role        string
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	cfg := Config{
		Addr:                    env("SLOPR_ADDR", ":8080"),
		DBPath:                  env("SLOPR_DB_PATH", "/data/slopr.db"),
		UsersDir:                env("SLOPR_USERS_DIR", "/data/users"),
		PublicURL:               env("SLOPR_PUBLIC_URL", ""),
		ChatBaseURL:             env("SLOPR_CHAT_BASE_URL", ""),
		ChatAPIKey:              env("SLOPR_CHAT_API_KEY", ""),
		ChatModel:               env("SLOPR_CHAT_MODEL", "MiMo"),
		ChatReasoningEffort:     env("SLOPR_CHAT_REASONING_EFFORT", "high"),
		ChatMaxCompletionTokens: defaultChatMaxCompletionTokens,
		ChatLogDir:              env("SLOPR_CHAT_LOG_DIR", "logs/llm-responses"),
		EmbedBaseURL:            env("SLOPR_EMBED_BASE_URL", ""),
		EmbedAPIKey:             env("SLOPR_EMBED_API_KEY", ""),
		EmbedModel:              env("SLOPR_EMBED_MODEL", "text-embedding-3-small"),
		BFLBaseURL:              env("SLOPR_BFL_BASE_URL", "https://api.bfl.ai/v1"),
		BFLAPIKey:               env("SLOPR_BFL_API_KEY", ""),
		BFLModel:                env("SLOPR_BFL_MODEL", "flux-2-klein-4b"),
		TikaURL:                 env("SLOPR_TIKA_URL", "http://tika:9998"),
		TavilyURL:               env("SLOPR_TAVILY_URL", "https://mcp.tavily.com/mcp/"),
		TavilyAPIKey:            env("SLOPR_TAVILY_API_KEY", ""),
		FetchMCPURL:             env("SLOPR_FETCH_MCP_URL", ""),
		ObscuraMCPURL:           env("SLOPR_OBSCURA_MCP_URL", ""),
		Context7MCPURL:          env("SLOPR_CONTEXT7_MCP_URL", "https://mcp.context7.com/mcp"),
		Context7APIKey:          env("SLOPR_CONTEXT7_API_KEY", ""),
		AdminInitialPassword:    env("SLOPR_ADMIN_INITIAL_PASSWORD", ""),
		SessionSecret:           env("SLOPR_SESSION_SECRET", ""),
		AuthMode:                AuthMode(env("SLOPR_AUTH_MODE", "")),
		OIDC: OIDCConfig{
			Issuer:                env("SLOPR_OIDC_ISSUER", ""),
			ClientID:              env("SLOPR_OIDC_CLIENT_ID", ""),
			ClientSecret:          env("SLOPR_OIDC_CLIENT_SECRET", ""),
			RedirectURL:           env("SLOPR_OIDC_REDIRECT_URL", ""),
			PostLogoutRedirectURL: env("SLOPR_OIDC_POST_LOGOUT_REDIRECT_URL", ""),
			AdminGroup:            env("SLOPR_OIDC_ADMIN_GROUP", ""),
		},
		DevUser: DevUserConfig{
			Subject:     env("SLOPR_DEV_USER_SUBJECT", "dev-admin"),
			Username:    env("SLOPR_DEV_USER_USERNAME", "dev"),
			Email:       env("SLOPR_DEV_USER_EMAIL", "dev@example.local"),
			DisplayName: env("SLOPR_DEV_USER_NAME", "Dev Admin"),
			Role:        "admin",
		},
	}
	bflPollTimeout, err := time.ParseDuration(env("SLOPR_BFL_POLL_TIMEOUT", defaultBFLPollTimeout.String()))
	if err != nil || bflPollTimeout <= 0 {
		return Config{}, fmt.Errorf("SLOPR_BFL_POLL_TIMEOUT must be a duration greater than 0")
	}
	cfg.BFLPollTimeout = bflPollTimeout
	maxCompletionTokens, err := strconv.Atoi(env("SLOPR_CHAT_MAX_COMPLETION_TOKENS", strconv.Itoa(defaultChatMaxCompletionTokens)))
	if err != nil || maxCompletionTokens <= 0 {
		return Config{}, fmt.Errorf("SLOPR_CHAT_MAX_COMPLETION_TOKENS must be an integer greater than 0")
	}
	cfg.ChatMaxCompletionTokens = maxCompletionTokens
	chatTimeout, err := time.ParseDuration(env("SLOPR_CHAT_TIMEOUT", defaultChatTimeout.String()))
	if err != nil || chatTimeout <= 0 {
		return Config{}, fmt.Errorf("SLOPR_CHAT_TIMEOUT must be a duration greater than 0")
	}
	cfg.ChatTimeout = chatTimeout
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("SLOPR_SESSION_SECRET is required")
	}
	if cfg.AuthMode == AuthModeNone && cfg.OIDC.Issuer != "" {
		cfg.AuthMode = AuthModeOIDC
	}
	switch cfg.AuthMode {
	case AuthModeNone:
	case AuthModeOIDC:
		if cfg.OIDC.Issuer == "" {
			return Config{}, fmt.Errorf("SLOPR_OIDC_ISSUER is required when SLOPR_AUTH_MODE=oidc")
		}
		if cfg.OIDC.ClientID == "" {
			return Config{}, fmt.Errorf("SLOPR_OIDC_CLIENT_ID is required when SLOPR_OIDC_ISSUER is set")
		}
		if cfg.OIDC.ClientSecret == "" {
			return Config{}, fmt.Errorf("SLOPR_OIDC_CLIENT_SECRET is required when SLOPR_OIDC_ISSUER is set")
		}
		if cfg.OIDC.RedirectURL == "" {
			return Config{}, fmt.Errorf("SLOPR_OIDC_REDIRECT_URL is required when SLOPR_OIDC_ISSUER is set")
		}
	case AuthModeDev:
		if err := validateDevAuthLocalOnly(cfg); err != nil {
			return Config{}, err
		}
	default:
		return Config{}, fmt.Errorf("SLOPR_AUTH_MODE must be one of: oidc, dev")
	}
	if cfg.Context7APIKey != "" && cfg.Context7MCPURL == "" {
		return Config{}, fmt.Errorf("SLOPR_CONTEXT7_MCP_URL is required when SLOPR_CONTEXT7_API_KEY is set")
	}
	if cfg.BFLAPIKey != "" {
		if cfg.BFLBaseURL == "" {
			return Config{}, fmt.Errorf("SLOPR_BFL_BASE_URL is required when SLOPR_BFL_API_KEY is set")
		}
		if cfg.BFLModel == "" {
			return Config{}, fmt.Errorf("SLOPR_BFL_MODEL is required when SLOPR_BFL_API_KEY is set")
		}
	}
	return cfg, nil
}

func validateDevAuthLocalOnly(cfg Config) error {
	if !isLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("SLOPR_AUTH_MODE=dev requires SLOPR_ADDR to bind to localhost or a loopback address")
	}
	if cfg.PublicURL != "" && !isLoopbackPublicURL(cfg.PublicURL) {
		return fmt.Errorf("SLOPR_AUTH_MODE=dev requires SLOPR_PUBLIC_URL to be empty or loopback")
	}
	return nil
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return isLoopbackHost(host)
}

func isLoopbackPublicURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return isLoopbackHost(parsed.Hostname())
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
