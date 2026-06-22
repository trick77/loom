// Package config loads loom's runtime configuration from environment variables.
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

// defaultKnowledgeInlineTokenBudget caps the total tokens of indexed knowledge
// documents auto-injected in full per chat turn. When the in-scope knowledge fits
// under this budget the whole of it goes inline (verbatim) and RAG retrieval is
// skipped; larger knowledge bases fall back to RAG excerpts. Sized to fit a
// typical briefing deck. Set BACKEND_KNOWLEDGE_INLINE_TOKEN_BUDGET=0 to disable
// full-document injection and always use RAG.
const defaultKnowledgeInlineTokenBudget = 24000

// defaultChatIdleTimeout aborts a chat stream that goes silent mid-turn. Sized
// conservatively: MiMo can stay quiet for tens of seconds inside a long reasoning
// block, so the window must clear that to avoid killing legitimate turns, while
// still being far below the total ChatTimeout so a true stall ends in seconds, not
// minutes. 60s gives ~8x headroom over the worst inter-chunk gap measured against
// real MiMo (~7.6s on a multi-minute reasoning turn). Set
// BACKEND_CHAT_IDLE_TIMEOUT=0 to disable the watchdog.
const defaultChatIdleTimeout = 60 * time.Second

// AuthMode selects how Loom signs users in.
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
	ChatMaxCompletionTokens int
	ChatTimeout             time.Duration
	ChatIdleTimeout         time.Duration
	ChatLogDir              string
	EmbedBaseURL            string // OpenAI embeddings endpoint
	EmbedAPIKey             string
	EmbedModel              string
	// KnowledgeInlineTokenBudget bounds the full-document knowledge injected per
	// turn (0 disables it, falling back to pure RAG retrieval).
	KnowledgeInlineTokenBudget int
	BFLBaseURL                 string
	BFLAPIKey                  string
	BFLModel                   string
	BFLPollTimeout             time.Duration

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
		Addr:                    env("BACKEND_ADDR", ":8080"),
		DBPath:                  env("BACKEND_DB_PATH", "/data/loom.db"),
		UsersDir:                env("BACKEND_USERS_DIR", "/data/users"),
		PublicURL:               env("BACKEND_PUBLIC_URL", ""),
		ChatBaseURL:             env("BACKEND_CHAT_BASE_URL", ""),
		ChatAPIKey:              env("BACKEND_CHAT_API_KEY", ""),
		ChatMaxCompletionTokens: defaultChatMaxCompletionTokens,
		ChatLogDir:              env("BACKEND_CHAT_LOG_DIR", "logs/llm-responses"),
		EmbedBaseURL:            env("BACKEND_EMBED_BASE_URL", ""),
		EmbedAPIKey:             env("BACKEND_EMBED_API_KEY", ""),
		EmbedModel:              env("BACKEND_EMBED_MODEL", "text-embedding-3-small"),
		BFLBaseURL:              env("BACKEND_BFL_BASE_URL", "https://api.bfl.ai/v1"),
		BFLAPIKey:               env("BACKEND_BFL_API_KEY", ""),
		BFLModel:                env("BACKEND_BFL_MODEL", "flux-2-klein-4b"),
		TikaURL:                 env("BACKEND_TIKA_URL", "http://tika:9998"),
		TavilyURL:               env("BACKEND_TAVILY_URL", "https://mcp.tavily.com/mcp/"),
		TavilyAPIKey:            env("BACKEND_TAVILY_API_KEY", ""),
		FetchMCPURL:             env("BACKEND_FETCH_MCP_URL", ""),
		ObscuraMCPURL:           env("BACKEND_OBSCURA_MCP_URL", ""),
		Context7MCPURL:          env("BACKEND_CONTEXT7_MCP_URL", "https://mcp.context7.com/mcp"),
		Context7APIKey:          env("BACKEND_CONTEXT7_API_KEY", ""),
		AdminInitialPassword:    env("BACKEND_ADMIN_INITIAL_PASSWORD", ""),
		SessionSecret:           env("BACKEND_SESSION_SECRET", ""),
		AuthMode:                AuthMode(env("BACKEND_AUTH_MODE", "")),
		OIDC: OIDCConfig{
			Issuer:                env("BACKEND_OIDC_ISSUER", ""),
			ClientID:              env("BACKEND_OIDC_CLIENT_ID", ""),
			ClientSecret:          env("BACKEND_OIDC_CLIENT_SECRET", ""),
			RedirectURL:           env("BACKEND_OIDC_REDIRECT_URL", ""),
			PostLogoutRedirectURL: env("BACKEND_OIDC_POST_LOGOUT_REDIRECT_URL", ""),
			AdminGroup:            env("BACKEND_OIDC_ADMIN_GROUP", ""),
		},
		DevUser: DevUserConfig{
			Subject:     env("BACKEND_DEV_USER_SUBJECT", "dev-admin"),
			Username:    env("BACKEND_DEV_USER_USERNAME", "dev"),
			Email:       env("BACKEND_DEV_USER_EMAIL", "dev@example.local"),
			DisplayName: env("BACKEND_DEV_USER_NAME", "Dev Admin"),
			Role:        "admin",
		},
	}
	bflPollTimeout, err := time.ParseDuration(env("BACKEND_BFL_POLL_TIMEOUT", defaultBFLPollTimeout.String()))
	if err != nil || bflPollTimeout <= 0 {
		return Config{}, fmt.Errorf("BACKEND_BFL_POLL_TIMEOUT must be a duration greater than 0")
	}
	cfg.BFLPollTimeout = bflPollTimeout
	maxCompletionTokens, err := strconv.Atoi(env("BACKEND_CHAT_MAX_COMPLETION_TOKENS", strconv.Itoa(defaultChatMaxCompletionTokens)))
	if err != nil || maxCompletionTokens <= 0 {
		return Config{}, fmt.Errorf("BACKEND_CHAT_MAX_COMPLETION_TOKENS must be an integer greater than 0")
	}
	cfg.ChatMaxCompletionTokens = maxCompletionTokens
	chatTimeout, err := time.ParseDuration(env("BACKEND_CHAT_TIMEOUT", defaultChatTimeout.String()))
	if err != nil || chatTimeout <= 0 {
		return Config{}, fmt.Errorf("BACKEND_CHAT_TIMEOUT must be a duration greater than 0")
	}
	cfg.ChatTimeout = chatTimeout
	chatIdleTimeout, err := time.ParseDuration(env("BACKEND_CHAT_IDLE_TIMEOUT", defaultChatIdleTimeout.String()))
	if err != nil || chatIdleTimeout < 0 {
		return Config{}, fmt.Errorf("BACKEND_CHAT_IDLE_TIMEOUT must be a non-negative duration (0 disables the idle watchdog)")
	}
	cfg.ChatIdleTimeout = chatIdleTimeout
	knowledgeInlineTokenBudget, err := strconv.Atoi(env("BACKEND_KNOWLEDGE_INLINE_TOKEN_BUDGET", strconv.Itoa(defaultKnowledgeInlineTokenBudget)))
	if err != nil || knowledgeInlineTokenBudget < 0 {
		return Config{}, fmt.Errorf("BACKEND_KNOWLEDGE_INLINE_TOKEN_BUDGET must be a non-negative integer (0 disables full-document knowledge injection)")
	}
	cfg.KnowledgeInlineTokenBudget = knowledgeInlineTokenBudget
	if cfg.SessionSecret == "" {
		return Config{}, fmt.Errorf("BACKEND_SESSION_SECRET is required")
	}
	if cfg.AuthMode == AuthModeNone && cfg.OIDC.Issuer != "" {
		cfg.AuthMode = AuthModeOIDC
	}
	switch cfg.AuthMode {
	case AuthModeNone:
	case AuthModeOIDC:
		if cfg.OIDC.Issuer == "" {
			return Config{}, fmt.Errorf("BACKEND_OIDC_ISSUER is required when BACKEND_AUTH_MODE=oidc")
		}
		if cfg.OIDC.ClientID == "" {
			return Config{}, fmt.Errorf("BACKEND_OIDC_CLIENT_ID is required when BACKEND_OIDC_ISSUER is set")
		}
		if cfg.OIDC.ClientSecret == "" {
			return Config{}, fmt.Errorf("BACKEND_OIDC_CLIENT_SECRET is required when BACKEND_OIDC_ISSUER is set")
		}
		if cfg.OIDC.RedirectURL == "" {
			return Config{}, fmt.Errorf("BACKEND_OIDC_REDIRECT_URL is required when BACKEND_OIDC_ISSUER is set")
		}
	case AuthModeDev:
		if err := validateDevAuthLocalOnly(cfg); err != nil {
			return Config{}, err
		}
	default:
		return Config{}, fmt.Errorf("BACKEND_AUTH_MODE must be one of: oidc, dev")
	}
	if cfg.Context7APIKey != "" && cfg.Context7MCPURL == "" {
		return Config{}, fmt.Errorf("BACKEND_CONTEXT7_MCP_URL is required when BACKEND_CONTEXT7_API_KEY is set")
	}
	if cfg.BFLAPIKey != "" {
		if cfg.BFLBaseURL == "" {
			return Config{}, fmt.Errorf("BACKEND_BFL_BASE_URL is required when BACKEND_BFL_API_KEY is set")
		}
		if cfg.BFLModel == "" {
			return Config{}, fmt.Errorf("BACKEND_BFL_MODEL is required when BACKEND_BFL_API_KEY is set")
		}
	}
	return cfg, nil
}

func validateDevAuthLocalOnly(cfg Config) error {
	if !isLoopbackAddr(cfg.Addr) {
		return fmt.Errorf("BACKEND_AUTH_MODE=dev requires BACKEND_ADDR to bind to localhost or a loopback address")
	}
	if cfg.PublicURL != "" && !isLoopbackPublicURL(cfg.PublicURL) {
		return fmt.Errorf("BACKEND_AUTH_MODE=dev requires BACKEND_PUBLIC_URL to be empty or loopback")
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
