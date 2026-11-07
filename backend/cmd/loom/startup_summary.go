package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/trick77/loom/internal/config"
	"github.com/trick77/loom/internal/mcp"
)

type startupRuntime struct {
	// DocgenToolCount is how many document tools discovery found.
	DocgenToolCount     int
	DiscoveredToolCount int
	// ImageToolUp: discovery found the image tool (a best-effort sidecar may
	// be configured and still not answering).
	ImageToolUp bool
	// SearchToolUp: discovery found the web search tool. SearchServerUp: its
	// server answered, with or without the tool.
	SearchToolUp   bool
	SearchServerUp bool
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
		docgenCapability(mcpConfig, runtime),
		mcpCapability(mcpConfig, runtime),
		mcpFileCapability(cfg),
		searchCapability(mcpConfig, runtime),
		imageGenCapability(mcpConfig, runtime),
		sandboxCapability(cfg),
		responseLoggingCapability(cfg),
	}
}

// config.Load accepts only oidc and dev.
func authCapability(cfg config.Config) startupCapability {
	if cfg.AuthMode == config.AuthModeOIDC {
		return startupCapability{Name: "auth", Status: "oidc", Detail: "issuer=" + cfg.OIDC.Issuer}
	}
	return startupCapability{Name: "auth", Status: "dev", Detail: "local loopback only"}
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
		return startupCapability{Name: "embeddings", Status: "disabled", Detail: "set " + cfg.EmbedMissing}
	}
	return startupCapability{Name: "embeddings", Status: "enabled", Detail: "model=" + cfg.EmbedModel.ID}
}

func tikaCapability(cfg config.Config) startupCapability {
	if strings.TrimSpace(cfg.TikaURL) == "" {
		return startupCapability{Name: "document extraction", Status: "disabled", Detail: "set BACKEND_TIKA_URL"}
	}
	return startupCapability{Name: "document extraction", Status: "enabled", Detail: "url=" + cfg.TikaURL}
}

// config.Load requires an absolute BACKEND_USERS_DIR, so artifacts are
// always on.
func artifactCapability(cfg config.Config) startupCapability {
	return startupCapability{Name: "artifacts", Status: "enabled", Detail: "users_dir=" + cfg.UsersDir}
}

// docgenCapability says who serves the document tools: the loom-docgen
// sidecar, or an mcp.json server named docgen that replaces it, and how many
// tools discovery found.
func docgenCapability(mcpConfig mcp.Config, runtime startupRuntime) startupCapability {
	server, ok := mcpConfig.Servers[mcp.DocgenServer]
	who := "mcp.json server " + mcp.DocgenServer
	if server.BuiltIn {
		who = "loom-docgen sidecar"
	}
	switch {
	case !ok:
		return startupCapability{Name: "document generation", Status: "disabled", Detail: "set BACKEND_DOCGEN_URL (loom-docgen sidecar)"}
	case !server.Artifacts:
		// Every file it returned would be dropped.
		return startupCapability{Name: "document generation", Status: "disabled", Detail: who + " lacks \"artifacts\": true"}
	case runtime.DocgenToolCount == 0:
		return startupCapability{Name: "document generation", Status: "configured", Detail: who + " not answering yet; offered once it does"}
	}
	return startupCapability{Name: "document generation", Status: "enabled", Detail: fmt.Sprintf("%s tools=%d", who, runtime.DocgenToolCount)}
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

// searchCapability says who serves web search: the loom-search sidecar, or
// an mcp.json server named search that replaces it, and whether it answered
// discovery. The sidecar answers without a provider key but serves no tool,
// which is not the same as not answering.
func searchCapability(mcpConfig mcp.Config, runtime startupRuntime) startupCapability {
	server, ok := mcpConfig.Servers[mcp.SearchServer]
	who := "mcp.json server " + mcp.SearchServer
	if server.BuiltIn {
		who = "loom-search sidecar"
	}
	switch {
	case !ok:
		return startupCapability{Name: "Web search", Status: "disabled", Detail: "set BACKEND_SEARCH_URL (loom-search sidecar)"}
	case runtime.SearchToolUp:
		return startupCapability{Name: "Web search", Status: "enabled", Detail: who}
	case runtime.SearchServerUp:
		// The sidecar serves the tool only with a provider key.
		return startupCapability{Name: "Web search", Status: "disabled", Detail: who + " serves no " + mcp.SearchTool + " tool (the sidecar's provider key unset?)"}
	}
	return startupCapability{Name: "Web search", Status: "configured", Detail: who + " not answering yet; offered once it does"}
}

// imageGenCapability says who serves the image tool: the loom-imagegen
// sidecar, or an mcp.json server named imagegen that replaces it, and whether
// it answered discovery.
func imageGenCapability(mcpConfig mcp.Config, runtime startupRuntime) startupCapability {
	server, ok := mcpConfig.Servers[mcp.ImagegenServer]
	who := "mcp.json server " + mcp.ImagegenServer
	if server.BuiltIn {
		who = "loom-imagegen sidecar"
	}
	switch {
	case !ok:
		return startupCapability{Name: "Image generation", Status: "disabled", Detail: "set BACKEND_IMAGE_GEN_URL (loom-imagegen sidecar)"}
	case !server.Artifacts:
		return startupCapability{Name: "Image generation", Status: "disabled", Detail: who + " lacks \"artifacts\": true"}
	case !runtime.ImageToolUp:
		return startupCapability{Name: "Image generation", Status: "configured", Detail: who + " not answering yet; offered once it does"}
	}
	return startupCapability{Name: "Image generation", Status: "enabled", Detail: who}
}

// sandboxCapability reports the configuration only: whether run_python is
// actually offered follows the sidecar's health probe at run time.
func sandboxCapability(cfg config.Config) startupCapability {
	if cfg.SandboxProblem != "" {
		return startupCapability{Name: "Python sandbox", Status: "misconfigured", Detail: cfg.SandboxProblem}
	}
	if cfg.SandboxURL == "" {
		return startupCapability{Name: "Python sandbox", Status: "disabled", Detail: "set BACKEND_SANDBOX_URL"}
	}
	return startupCapability{Name: "Python sandbox", Status: "configured", Detail: fmt.Sprintf("url=%s timeout=%s (offered while healthy)", cfg.SandboxURL, cfg.SandboxTimeout)}
}

func responseLoggingCapability(cfg config.Config) startupCapability {
	if responseLogDirForConfig(cfg) == "" {
		return startupCapability{Name: "LLM response logging", Status: "disabled", Detail: "enabled only in dev auth mode"}
	}
	return startupCapability{Name: "LLM response logging", Status: "enabled", Detail: "dir=" + cfg.ChatLogDir}
}
