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
		Addr:                 env("SPARK_ADDR", ":8080"),
		DBPath:               env("SPARK_DB_PATH", "/data/spark.db"),
		UsersDir:             env("SPARK_USERS_DIR", "/data/users"),
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
	}
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("SPARK_SESSION_SECRET is required")
	}
	if cfg.AdminInitialPassword == "" {
		return Config{}, fmt.Errorf("SPARK_ADMIN_INITIAL_PASSWORD is required")
	}
	return cfg, nil
}
