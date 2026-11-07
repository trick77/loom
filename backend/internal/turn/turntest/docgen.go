package turntest

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
	"github.com/trick77/toolkit/mcpserver"
)

// DocgenService is the document tools as loom reaches them in production:
// the loom-docgen MCP server (docgen.MCPServer) over HTTP, registered as the
// built-in sidecar. It serves every generator but the PDF one, which needs
// Gotenberg. extra servers ride along (e.g. fetch).
func DocgenService(t testing.TB, extra ...map[string]mcp.ServerConfig) *mcp.Service {
	t.Helper()
	gens := []docgen.Generator{docgen.TextGenerator{}, docgen.XLSXGenerator{}, docgen.DOCXGenerator{}, docgen.PPTXGenerator{}}
	srv := httptest.NewServer(mcpserver.New(docgen.MCPServer(gens, "test")))
	t.Cleanup(srv.Close)
	required := mcp.Config{Servers: map[string]mcp.ServerConfig{}}
	for _, servers := range extra {
		for name, sc := range servers {
			required.Servers[name] = sc
		}
	}
	sidecar := mcp.Config{Servers: map[string]mcp.ServerConfig{
		mcp.DocgenServer: mcp.DocgenServerConfig(srv.URL, "test-token"),
	}}
	svc, err := mcp.NewServiceFromConfigs(context.Background(), required, sidecar, srv.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// DocgenTool is a document tool's exposed name, e.g. DocgenTool("create_text_file").
func DocgenTool(name string) string {
	return mcp.ExposedToolName(mcp.DocgenServer, name)
}

// DocgenArtifacts marks the docgen server as one that makes artifacts, for a
// fake ToolService's Artifacts: loom offers its tools only then.
func DocgenArtifacts() map[string]bool {
	return map[string]bool{mcp.DocgenServer: true}
}

// DocgenToolDef is a document tool as an MCP tool list carries it, for a fake
// ToolService.
func DocgenToolDef(name string) llm.Tool {
	return llm.Tool{Type: "function", Function: llm.ToolFunction{Name: DocgenTool(name), Parameters: map[string]any{"type": "object"}}}
}
