// Package mcp contains Spark's MCP client configuration and tool registry.
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	TransportStreamableHTTP = "streamable-http"
	TransportStdio          = "stdio"
)

// Config is the on-disk MCP server configuration loaded from mcp.json.
type Config struct {
	Servers map[string]ServerConfig `json:"servers"`
}

// ServerConfig describes one MCP server.
type ServerConfig struct {
	Transport string            `json:"transport"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
}

func LoadConfig(path string) (Config, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(bytes, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode MCP config: %w", err)
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]ServerConfig{}
	}
	for name, server := range cfg.Servers {
		normalized, err := normalizeServerConfig(name, server)
		if err != nil {
			return Config{}, err
		}
		cfg.Servers[name] = normalized
	}
	return cfg, nil
}

func normalizeServerConfig(name string, server ServerConfig) (ServerConfig, error) {
	if strings.TrimSpace(name) == "" {
		return ServerConfig{}, fmt.Errorf("MCP server name is required")
	}
	switch server.Transport {
	case "", "http", TransportStreamableHTTP:
		server.Transport = TransportStreamableHTTP
		if strings.TrimSpace(server.URL) == "" {
			return ServerConfig{}, fmt.Errorf("MCP server %q requires url", name)
		}
	case TransportStdio:
		if strings.TrimSpace(server.Command) == "" {
			return ServerConfig{}, fmt.Errorf("MCP server %q requires command", name)
		}
	default:
		return ServerConfig{}, fmt.Errorf("MCP server %q has unsupported transport %q", name, server.Transport)
	}
	if server.Headers == nil {
		server.Headers = map[string]string{}
	}
	if server.Env == nil {
		server.Env = map[string]string{}
	}
	return server, nil
}

func ExposedToolName(serverName, toolName string) string {
	return serverName + "__" + toolName
}

func SplitExposedToolName(name string) (string, string, bool) {
	server, tool, ok := strings.Cut(name, "__")
	if !ok || server == "" || tool == "" {
		return "", "", false
	}
	return server, tool, true
}
