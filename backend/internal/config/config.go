// Package config loads spark's runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime settings. Secrets come from ENV only.
type Config struct {
	Addr     string // HTTP listen address
	DBPath   string // path to the SQLite file
	UsersDir string // root for per-user volumes: <UsersDir>/<user-id>/
	PublicURL string // externally reachable base URL

	ChatBaseURL  string // OpenAI-compatible chat endpoint (MiMo)
	ChatAPIKey   string
	ChatModel    string
	EmbedBaseURL string // OpenAI embeddings endpoint
	EmbedAPIKey  string
	EmbedModel   string

	TikaURL       string
	SearxngURL    string
	MCPConfigPath string

	AdminInitialPassword string // legacy; authentik owns credentials in Phase 2
	SessionSecret        string
	OIDC                 OIDCConfig
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
		EmbedBaseURL:         env("SPARK_EMBED_BASE_URL", ""),
		EmbedAPIKey:          env("SPARK_EMBED_API_KEY", ""),
		EmbedModel:           env("SPARK_EMBED_MODEL", "text-embedding-3-small"),
		TikaURL:              env("SPARK_TIKA_URL", "http://tika:9998"),
		SearxngURL:           env("SPARK_SEARXNG_URL", "http://searxng:8080"),
		MCPConfigPath:        env("SPARK_MCP_CONFIG", "/config/mcp.json"),
		AdminInitialPassword: env("SPARK_ADMIN_INITIAL_PASSWORD", ""),
		SessionSecret:        env("SPARK_SESSION_SECRET", ""),
		OIDC: OIDCConfig{
			Issuer:                env("SPARK_OIDC_ISSUER", ""),
			ClientID:              env("SPARK_OIDC_CLIENT_ID", ""),
			ClientSecret:          env("SPARK_OIDC_CLIENT_SECRET", ""),
			RedirectURL:           env("SPARK_OIDC_REDIRECT_URL", ""),
			PostLogoutRedirectURL: env("SPARK_OIDC_POST_LOGOUT_REDIRECT_URL", ""),
			AdminGroup:            env("SPARK_OIDC_ADMIN_GROUP", ""),
		},
	}
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("SPARK_SESSION_SECRET is required")
	}
	return cfg, nil
}
