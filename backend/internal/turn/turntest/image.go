package turntest

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/trick77/loom/internal/imagegen"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/toolkit/mcpserver"
)

// ImageProvider is an image provider that returns a tiny fake PNG.
type ImageProvider struct{}

// Generate implements imagegen.Provider.
func (ImageProvider) Generate(_ context.Context, req imagegen.GenerateRequest) (imagegen.GenerateResult, error) {
	model := req.Model
	if model == "" {
		model = "fake-model"
	}
	return imagegen.GenerateResult{
		Filename:  req.Filename,
		Extension: "png",
		MIMEType:  "image/png",
		Bytes:     []byte("\x89PNG\r\n\x1a\nfake"),
		Provider:  "fake",
		Model:     model,
		RequestID: "request-1",
		Prompt:    req.Prompt,
		Width:     req.Width,
		Height:    req.Height,
	}, nil
}

// ImageService is the image tool as loom reaches it in production: the
// loom-imagegen MCP server (imagegen.MCPServer) over HTTP, registered as the
// built-in sidecar. provider renders; defaultModel and typographyModel are the
// server's models ("" for none). extra servers ride along (e.g. fetch).
func ImageService(t testing.TB, provider imagegen.Provider, defaultModel, typographyModel string, extra ...map[string]mcp.ServerConfig) *mcp.Service {
	t.Helper()
	srv := httptest.NewServer(mcpserver.New(imagegen.MCPServer(imagegen.NewService(provider, defaultModel, typographyModel), "test")))
	t.Cleanup(srv.Close)
	required := mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	for _, servers := range extra {
		for name, sc := range servers {
			required.Servers[name] = sc
		}
	}
	sidecar := mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.ImagegenServer: mcp.ImagegenServerConfig(srv.URL, "test-token"),
	}}
	svc, err := mcp.NewServiceFromConfigs(context.Background(), required, sidecar, srv.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}
