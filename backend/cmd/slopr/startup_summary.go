package main

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/trick77/slopr/internal/config"
	"github.com/trick77/slopr/internal/llm"
	"github.com/trick77/slopr/internal/mcp"
)

type startupRuntime struct {
	DocToolCount        int
	ImageToolCount      int
	DiscoveredToolCount int
}

type startupCapability struct {
	Name   string
	Status string
	Detail string
}

func logStartupCapabilities(cfg config.Config, mcpConfig mcp.Config, runtime startupRuntime) {
	for _, item := range startupCapabilities(cfg, mcpConfig, runtime) {
		slog.Info("startup capability", "name", item.Name, "status", item.Status, "detail", item.Detail)
	}
}

func startupCapabilities(cfg config.Config, mcpConfig mcp.Config, runtime startupRuntime) []startupCapability {
	return []startupCapability{
		authCapability(cfg),
		chatCapability(cfg),
		embeddingsCapability(cfg),
		tikaCapability(cfg),
		artifactCapability(cfg),
		docgenCapability(runtime),
		mcpCapability(mcpConfig, runtime),
		tavilyCapability(cfg),
		context7Capability(cfg),
		bflImageCapability(cfg, runtime),
		responseLoggingCapability(cfg),
	}
}

func authCapability(cfg config.Config) startupCapability {
	switch cfg.AuthMode {
	case config.AuthModeOIDC:
		return startupCapability{Name: "auth", Status: "oidc", Detail: "issuer=" + cfg.OIDC.Issuer}
	case config.AuthModeDev:
		return startupCapability{Name: "auth", Status: "dev", Detail: "local loopback only"}
	default:
		return startupCapability{Name: "auth", Status: "local", Detail: "server-side sessions"}
	}
}

func chatCapability(cfg config.Config) startupCapability {
	if strings.TrimSpace(cfg.ChatBaseURL) == "" {
		return startupCapability{Name: "chat", Status: "disabled", Detail: "set BACKEND_CHAT_BASE_URL"}
	}
	return startupCapability{Name: "chat", Status: "enabled", Detail: fmt.Sprintf("model=%s base_url=%s", llm.ModelSummary(), cfg.ChatBaseURL)}
}

func embeddingsCapability(cfg config.Config) startupCapability {
	if strings.TrimSpace(cfg.EmbedBaseURL) == "" || strings.TrimSpace(cfg.EmbedAPIKey) == "" {
		return startupCapability{Name: "embeddings", Status: "disabled", Detail: "set BACKEND_EMBED_BASE_URL and BACKEND_EMBED_API_KEY"}
	}
	return startupCapability{Name: "embeddings", Status: "enabled", Detail: fmt.Sprintf("model=%s base_url=%s", cfg.EmbedModel, cfg.EmbedBaseURL)}
}

func tikaCapability(cfg config.Config) startupCapability {
	if strings.TrimSpace(cfg.TikaURL) == "" {
		return startupCapability{Name: "document extraction", Status: "disabled", Detail: "set BACKEND_TIKA_URL"}
	}
	return startupCapability{Name: "document extraction", Status: "enabled", Detail: "url=" + cfg.TikaURL}
}

func artifactCapability(cfg config.Config) startupCapability {
	if strings.TrimSpace(cfg.UsersDir) == "" {
		return startupCapability{Name: "artifacts", Status: "disabled", Detail: "set BACKEND_USERS_DIR"}
	}
	return startupCapability{Name: "artifacts", Status: "enabled", Detail: "users_dir=" + cfg.UsersDir}
}

func docgenCapability(runtime startupRuntime) startupCapability {
	if runtime.DocToolCount == 0 {
		return startupCapability{Name: "document generation", Status: "disabled", Detail: "no built-in document tools"}
	}
	return startupCapability{Name: "document generation", Status: "enabled", Detail: fmt.Sprintf("tools=%d", runtime.DocToolCount)}
}

func mcpCapability(mcpConfig mcp.Config, runtime startupRuntime) startupCapability {
	if len(mcpConfig.Servers) == 0 {
		return startupCapability{Name: "MCP tools", Status: "disabled", Detail: "no configured MCP servers"}
	}
	return startupCapability{Name: "MCP tools", Status: "enabled", Detail: fmt.Sprintf("servers=%d discovered_tools=%d", len(mcpConfig.Servers), runtime.DiscoveredToolCount)}
}

func tavilyCapability(cfg config.Config) startupCapability {
	if !tavilyConfigured(cfg) {
		return startupCapability{Name: "Tavily web search", Status: "disabled", Detail: "set BACKEND_TAVILY_API_KEY"}
	}
	return startupCapability{Name: "Tavily web search", Status: "enabled", Detail: "source=env"}
}

func context7Capability(cfg config.Config) startupCapability {
	if !context7Configured(cfg) {
		return startupCapability{Name: "Context7 docs", Status: "disabled", Detail: "set BACKEND_CONTEXT7_API_KEY"}
	}
	return startupCapability{Name: "Context7 docs", Status: "enabled", Detail: "source=env"}
}

func bflImageCapability(cfg config.Config, runtime startupRuntime) startupCapability {
	if !bflImageConfigured(cfg) {
		return startupCapability{Name: "BFL image generation", Status: "disabled", Detail: "set BACKEND_BFL_API_KEY"}
	}
	return startupCapability{Name: "BFL image generation", Status: "enabled", Detail: fmt.Sprintf("model=%s tools=%d", cfg.BFLModel, runtime.ImageToolCount)}
}

func responseLoggingCapability(cfg config.Config) startupCapability {
	if responseLogDirForConfig(cfg) == "" {
		return startupCapability{Name: "LLM response logging", Status: "disabled", Detail: "enabled only in dev auth mode"}
	}
	return startupCapability{Name: "LLM response logging", Status: "enabled", Detail: "dir=" + cfg.ChatLogDir}
}
