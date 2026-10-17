package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// A server limited to other accounts is left out of the /tools catalog and
// the /mcp status panel. The allowlist holds account keys, never usernames.
func TestMCPHandlers_hideServersLimitedToOtherUsers(t *testing.T) {
	tools := []llm.Tool{
		{Type: "function", Function: llm.ToolFunction{Name: "peeq__search"}},
		{Type: "function", Function: llm.ToolFunction{Name: "ipverse__whois"}},
	}
	servers := []mcp.ServerStatus{{Name: "peeq", Active: true}, {Name: "ipverse", Active: true}}
	for _, c := range []struct {
		users []string
		want  int
	}{
		{users: []string{testUser.ID}, want: 2},
		{users: []string{testUser.Username}, want: 1},
		{users: []string{"user_other"}, want: 1},
	} {
		srv := newAuthenticatedServer(t, Deps{MCP: fakeMCPService{ToolList: tools, Servers: servers, Users: map[string][]string{"peeq": c.users}}})

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/mcp/tools", ""))
		var gotTools struct {
			Tools []mcpToolInfo `json:"tools"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &gotTools); err != nil {
			t.Fatalf("decode tools: %v", err)
		}
		if len(gotTools.Tools) != c.want {
			t.Fatalf("users %v: tools = %+v, want %d", c.users, gotTools.Tools, c.want)
		}

		rec = httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/mcp/servers", ""))
		var gotServers struct {
			Servers []mcp.ServerStatus `json:"servers"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &gotServers); err != nil {
			t.Fatalf("decode servers: %v", err)
		}
		if len(gotServers.Servers) != c.want {
			t.Fatalf("users %v: servers = %+v, want %d", c.users, gotServers.Servers, c.want)
		}
	}
}
