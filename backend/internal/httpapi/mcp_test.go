package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

func TestHandleMCPServers_returnsStatusSnapshot(t *testing.T) {
	srv := newAuthenticatedServer(t, Deps{
		MCP: fakeMCPService{servers: []mcp.ServerStatus{
			{Name: "ipverse", Active: true, Transport: "streamable-http", Endpoint: "gateway.ipverse.net", Origin: mcp.OriginFile, ToolCount: 2},
			{Name: "obscura", Active: false, Transport: "streamable-http", Endpoint: "obscura:8090", Origin: mcp.OriginBuiltIn, ToolCount: 0, Error: "dial tcp: connection refused"},
		}},
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/mcp/servers", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Servers []mcp.ServerStatus `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Servers) != 2 {
		t.Fatalf("servers = %d, want 2: %s", len(got.Servers), rec.Body.String())
	}
	if got.Servers[0].Origin != mcp.OriginFile || got.Servers[0].ToolCount != 2 {
		t.Fatalf("ipverse status not surfaced: %+v", got.Servers[0])
	}
	if got.Servers[1].Active || got.Servers[1].Error == "" {
		t.Fatalf("down server should carry Active=false + Error: %+v", got.Servers[1])
	}
}

func TestHandleMCPServers_emptyIsAlwaysArray(t *testing.T) {
	srv := newAuthenticatedServer(t, Deps{MCP: fakeMCPService{}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/mcp/servers", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// A nil status slice must still serialize as [] so the UI can iterate.
	var got struct {
		Servers []mcp.ServerStatus `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Servers == nil {
		t.Fatalf("servers must be [] not null: %s", rec.Body.String())
	}
}

func TestHandleMCPTools_splitsServerAndRequiredArgs(t *testing.T) {
	srv := newAuthenticatedServer(t, Deps{
		MCP: fakeMCPService{tools: []llm.Tool{
			{Type: "function", Function: llm.ToolFunction{
				Name:        "ipverse__whois",
				Description: "Batch WHOIS lookup",
				Parameters:  map[string]any{"required": []any{"queries"}},
			}},
			{Type: "function", Function: llm.ToolFunction{
				Name:        "context7__query-docs",
				Description: "Fetch library docs",
			}},
		}},
	})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/mcp/tools", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Tools []mcpToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(got.Tools))
	}
	if got.Tools[0].Server != "ipverse" || len(got.Tools[0].Required) != 1 || got.Tools[0].Required[0] != "queries" {
		t.Fatalf("whois row wrong: %+v", got.Tools[0])
	}
	if got.Tools[1].Server != "context7" || got.Tools[1].Required != nil {
		t.Fatalf("docs row wrong: %+v", got.Tools[1])
	}
}

func TestHandleMCPServers_requiresAuth(t *testing.T) {
	srv := New(Deps{MCP: fakeMCPService{}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/mcp/servers", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
