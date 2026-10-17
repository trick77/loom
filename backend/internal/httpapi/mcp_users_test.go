package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/loom/internal/llm"
)

// A server limited to other accounts is left out of the /tools catalog.
func TestHandleMCPTools_hidesServersLimitedToOtherUsers(t *testing.T) {
	tools := []llm.Tool{
		{Type: "function", Function: llm.ToolFunction{Name: "peeq__search"}},
		{Type: "function", Function: llm.ToolFunction{Name: "ipverse__whois"}},
	}
	for _, c := range []struct {
		users []string
		want  int
	}{
		{users: []string{"jan"}, want: 2},
		{users: []string{"eve"}, want: 1},
	} {
		srv := newAuthenticatedServer(t, Deps{MCP: fakeMCPService{ToolList: tools, Users: map[string][]string{"peeq": c.users}}})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/mcp/tools", ""))
		var got struct {
			Tools []mcpToolInfo `json:"tools"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(got.Tools) != c.want {
			t.Fatalf("users %v: tools = %+v, want %d", c.users, got.Tools, c.want)
		}
	}
}
