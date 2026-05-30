// Package config loads eve's runtime configuration from environment variables.
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

	ChatBaseURL  string // OpenAI-compatible chat endpoint (MiMo)
	ChatAPIKey   string
	ChatModel    string
	EmbedBaseURL string // OpenAI embeddings endpoint
	EmbedAPIKey  string
	EmbedModel   string

	TikaURL       string
	SearxngURL    string
	MCPConfigPath string

	AdminInitialPassword string
	SessionSecret        string
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
		Addr:                 env("EVE_ADDR", ":8080"),
		DBPath:               env("EVE_DB_PATH", "/data/eve.db"),
		UsersDir:             env("EVE_USERS_DIR", "/data/users"),
		ChatBaseURL:          env("EVE_CHAT_BASE_URL", ""),
		ChatAPIKey:           env("EVE_CHAT_API_KEY", ""),
		ChatModel:            env("EVE_CHAT_MODEL", "MiMo"),
		EmbedBaseURL:         env("EVE_EMBED_BASE_URL", ""),
		EmbedAPIKey:          env("EVE_EMBED_API_KEY", ""),
		EmbedModel:           env("EVE_EMBED_MODEL", "text-embedding-3-small"),
		TikaURL:              env("EVE_TIKA_URL", "http://tika:9998"),
		SearxngURL:           env("EVE_SEARXNG_URL", "http://searxng:8080"),
		MCPConfigPath:        env("EVE_MCP_CONFIG", "/config/mcp.json"),
		AdminInitialPassword: env("EVE_ADMIN_INITIAL_PASSWORD", ""),
		SessionSecret:        env("EVE_SESSION_SECRET", ""),
	}
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("EVE_SESSION_SECRET is required")
	}
	if cfg.AdminInitialPassword == "" {
		return Config{}, fmt.Errorf("EVE_ADMIN_INITIAL_PASSWORD is required")
	}
	return cfg, nil
}
