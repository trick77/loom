package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/trick77/loom/internal/config"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/loom/internal/rag"
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
	// A model and its keys turn chat on, so a missing variable no longer fails
	// boot the way a missing endpoint did: outside dev auth that is a
	// deployment that cannot chat, and it must not pass as a routine line.
	if !cfg.ChatEnabled && cfg.AuthMode != config.AuthModeDev {
		slog.Warn("chat disabled: " + cfg.ChatMissing + " is unset, every turn will fail")
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
		mcpFileCapability(cfg),
		tavilyCapability(cfg),
		imageGenCapability(cfg, runtime),
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
	if !cfg.ChatEnabled {
		return startupCapability{Name: "chat", Status: "disabled", Detail: "set " + cfg.ChatMissing}
	}
	roles := cfg.ChatModels.Roles
	return startupCapability{Name: "chat", Status: "enabled",
		Detail: "model=" + roles.Chat + " gate=" + roles.Gate + " vision=" + roles.Vision}
}

func embeddingsCapability(cfg config.Config) startupCapability {
	if !cfg.EmbedEnabled {
		return startupCapability{Name: "embeddings", Status: "disabled", Detail: "set " + rag.EmbedAPIKeyEnv()}
	}
	return startupCapability{Name: "embeddings", Status: "enabled", Detail: "model=" + rag.EmbedModel}
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

func mcpFileCapability(cfg config.Config) startupCapability {
	path := strings.TrimSpace(cfg.MCPServersFile)
	if path == "" {
		return startupCapability{Name: "MCP servers file", Status: "disabled", Detail: "set BACKEND_MCP_SERVERS_FILE"}
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return startupCapability{Name: "MCP servers file", Status: "disabled", Detail: "no file at " + path}
	}
	return startupCapability{Name: "MCP servers file", Status: "enabled", Detail: "file=" + path}
}

func tavilyCapability(cfg config.Config) startupCapability {
	if !tavilyConfigured(cfg) {
		return startupCapability{Name: "Tavily web search", Status: "disabled", Detail: "set BACKEND_TAVILY_API_KEY"}
	}
	return startupCapability{Name: "Tavily web search", Status: "enabled", Detail: "source=env"}
}

func imageGenCapability(cfg config.Config, runtime startupRuntime) startupCapability {
	if !imageGenConfigured(cfg) {
		return startupCapability{Name: "Image generation", Status: "disabled", Detail: "set BACKEND_IMAGE_GEN_API_KEY"}
	}
	return startupCapability{Name: "Image generation", Status: "enabled", Detail: fmt.Sprintf("model=%s tools=%d", cfg.ImageGenModel, runtime.ImageToolCount)}
}

func responseLoggingCapability(cfg config.Config) startupCapability {
	if responseLogDirForConfig(cfg) == "" {
		return startupCapability{Name: "LLM response logging", Status: "disabled", Detail: "enabled only in dev auth mode"}
	}
	return startupCapability{Name: "LLM response logging", Status: "enabled", Detail: "dir=" + cfg.ChatLogDir}
}
