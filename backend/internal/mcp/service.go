package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/trick77/spark/internal/llm"
)

type Service struct {
	tools  []llm.Tool
	routes map[string]toolRoute
}

type toolRoute struct {
	client Client
	name   string
}

func NewService(clients map[string]Client) (*Service, error) {
	service := &Service{routes: map[string]toolRoute{}}
	names := make([]string, 0, len(clients))
	for name := range clients {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, serverName := range names {
		client := clients[serverName]
		tools, err := client.ListTools(context.Background())
		if err != nil {
			return nil, fmt.Errorf("list MCP tools for %s: %w", serverName, err)
		}
		for _, tool := range tools {
			if _, exists := service.routes[tool.Name]; exists {
				return nil, fmt.Errorf("duplicate MCP tool name %q", tool.Name)
			}
			service.routes[tool.Name] = toolRoute{client: client, name: tool.OriginalName}
			service.tools = append(service.tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
	}
	return service, nil
}

func NewServiceFromConfig(cfg Config, httpClient *http.Client) (*Service, error) {
	clients := map[string]Client{}
	for name, server := range cfg.Servers {
		clients[name] = clientForServer(name, server, httpClient)
	}
	return NewService(clients)
}

func NewBestEffortServiceFromConfig(ctx context.Context, cfg Config, httpClient *http.Client, logger *slog.Logger) (*Service, error) {
	service := &Service{routes: map[string]toolRoute{}}
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, serverName := range names {
		client := clientForServer(serverName, cfg.Servers[serverName], httpClient)
		tools, err := client.ListTools(ctx)
		if err != nil {
			if logger != nil {
				logger.Warn("MCP server discovery failed", "server", serverName, "err", err)
			}
			_ = client.Close()
			continue
		}
		for _, tool := range tools {
			if _, exists := service.routes[tool.Name]; exists {
				return nil, fmt.Errorf("duplicate MCP tool name %q", tool.Name)
			}
			service.routes[tool.Name] = toolRoute{client: client, name: tool.OriginalName}
			service.tools = append(service.tools, llm.Tool{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  tool.InputSchema,
				},
			})
		}
	}
	return service, nil
}

func clientForServer(name string, server ServerConfig, httpClient *http.Client) Client {
	if server.Transport == TransportStdio {
		return NewStdioClient(name, server)
	}
	return NewRemoteClient(name, server, httpClient)
}

func (s *Service) Tools() []llm.Tool {
	if s == nil {
		return nil
	}
	return append([]llm.Tool(nil), s.tools...)
}

func (s *Service) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	route, ok := s.routes[name]
	if !ok {
		return "", fmt.Errorf("unknown MCP tool %q", name)
	}
	return route.client.CallTool(ctx, route.name, arguments)
}
