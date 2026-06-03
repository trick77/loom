// Package config loads spark's runtime configuration from environment variables.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
)

// AuthMode selects how Spark signs users in.
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

	ChatBaseURL         string // OpenAI-compatible chat endpoint (MiMo)
	ChatAPIKey          string
	ChatModel           string
	ChatReasoningEffort string
	ChatLogDir          string
	EmbedBaseURL        string // OpenAI embeddings endpoint
	EmbedAPIKey         string
	EmbedModel          string

	TikaURL        string
	TavilyURL      string // hosted Tavily MCP endpoint for built-in web search
	TavilyAPIKey   string // enables built-in Tavily web search when set
	Context7MCPURL string
	Context7APIKey string
	MCPConfigPath  string

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
		Addr:                 env("SPARK_ADDR", ":8080"),
		DBPath:               env("SPARK_DB_PATH", "/data/spark.db"),
		UsersDir:             env("SPARK_USERS_DIR", "/data/users"),
		PublicURL:            env("SPARK_PUBLIC_URL", ""),
		ChatBaseURL:          env("SPARK_CHAT_BASE_URL", ""),
		ChatAPIKey:           env("SPARK_CHAT_API_KEY", ""),
		ChatModel:            env("SPARK_CHAT_MODEL", "MiMo"),
		ChatReasoningEffort:  env("SPARK_CHAT_REASONING_EFFORT", "high"),
		ChatLogDir:           env("SPARK_CHAT_LOG_DIR", "logs/llm-responses"),
		EmbedBaseURL:         env("SPARK_EMBED_BASE_URL", ""),
		EmbedAPIKey:          env("SPARK_EMBED_API_KEY", ""),
		EmbedModel:           env("SPARK_EMBED_MODEL", "text-embedding-3-small"),
		TikaURL:              env("SPARK_TIKA_URL", "http://tika:9998"),
		TavilyURL:            env("SPARK_TAVILY_URL", "https://mcp.tavily.com/mcp/"),
		TavilyAPIKey:         env("SPARK_TAVILY_API_KEY", ""),
		Context7MCPURL:       env("SPARK_CONTEXT7_MCP_URL", "https://mcp.context7.com/mcp"),
		Context7APIKey:       env("SPARK_CONTEXT7_API_KEY", ""),
		MCPConfigPath:        env("SPARK_MCP_CONFIG", "/config/mcp.json"),
		AdminInitialPassword: env("SPARK_ADMIN_INITIAL_PASSWORD", ""),
		SessionSecret:        env("SPARK_SESSION_SECRET", ""),
		AuthMode:             AuthMode(env("SPARK_AUTH_MODE", "")),
		OIDC: OIDCConfig{
			Issuer:                env("SPARK_OIDC_ISSUER", ""),
			ClientID:              env("SPARK_OIDC_CLIENT_ID", ""),
			ClientSecret:          env("SPARK_OIDC_CLIENT_SECRET", ""),
			RedirectURL:           env("SPARK_OIDC_REDIRECT_URL", ""),
			PostLogoutRedirectURL: env("SPARK_OIDC_POST_LOGOUT_REDIRECT_URL", ""),
			AdminGroup:            env("SPARK_OIDC_ADMIN_GROUP", ""),
		},
		DevUser: DevUserConfig{
			Subject:     env("SPARK_DEV_USER_SUBJECT", "dev-admin"),
			Username:    env("SPARK_DEV_USER_USERNAME", "dev"),
			Email:       env("SPARK_DEV_USER_EMAIL", "dev@example.local"),
			DisplayName: env("SPARK_DEV_USER_NAME", "Dev Admin"),
			Role:        "admin",
		},
	}
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("SPARK_SESSION_SECRET is required")
	}
	if cfg.AuthMode == AuthModeNone && cfg.OIDC.Issuer != "" {
		cfg.AuthMode = AuthModeOIDC
	}
	switch cfg.AuthMode {
	case AuthModeNone:
	case AuthModeOIDC:
		if cfg.OIDC.Issuer == "" {
			return Config{}, fmt.Errorf("SPARK_OIDC_ISSUER is required when SPARK_AUTH_MODE=oidc")
		}
		if cfg.OIDC.ClientID == "" {
			return Config{}, fmt.Errorf("SPARK_OIDC_CLIENT_ID is required when SPARK_OIDC_ISSUER is set")
		}
		if cfg.OIDC.ClientSecret == "" {
			return Config{}, fmt.Errorf("SPARK_OIDC_CLIENT_SECRET is required when SPARK_OIDC_ISSUER is set")
		}
		if cfg.OIDC.RedirectURL == "" {
			return Config{}, fmt.Errorf("SPARK_OIDC_REDIRECT_URL is required when SPARK_OIDC_ISSUER is set")
		}
	case AuthModeDev:
		if err := validateDevAuthLocalOnly(cfg); err != nil {
			return Config{}, err
		}
	default:
		return Config{}, fmt.Errorf("SPARK_AUTH_MODE must be one of: oidc, dev")
	}
	if cfg.Context7APIKey != "" && cfg.Context7MCPURL == "" {
		return Config{}, fmt.Errorf("SPARK_CONTEXT7_MCP_URL is required when SPARK_CONTEXT7_API_KEY is set")
	}
	return cfg, nil
}

func validateDevAuthLocalOnly(cfg Config) error {
	if !isLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("SPARK_AUTH_MODE=dev requires SPARK_ADDR to bind to localhost or a loopback address")
	}
	if cfg.PublicURL != "" && !isLoopbackPublicURL(cfg.PublicURL) {
		return fmt.Errorf("SPARK_AUTH_MODE=dev requires SPARK_PUBLIC_URL to be empty or loopback")
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
