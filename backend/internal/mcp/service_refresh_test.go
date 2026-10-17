package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// toggleServer is a modern server whose availability and tool name a test
// flips between refreshes.
func toggleServer(t *testing.T, up *atomic.Bool, toolName *atomic.Value) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		var req modernRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "server/discover":
			result = map[string]any{"resultType": "complete", "supportedVersions": []string{modernProtocolVersion}, "instructions": "hint for " + toolName.Load().(string)}
		case "tools/list":
			result = map[string]any{"resultType": "complete", "ttlMs": 120000, "tools": []map[string]any{{"name": toolName.Load().(string), "inputSchema": map[string]any{"type": "object"}}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestRefreshRegistersLateServerAndPicksUpChanges(t *testing.T) {
	var up atomic.Bool
	var name atomic.Value
	name.Store("search")
	server := toggleServer(t, &up, &name)
	bestEffort := Config{Servers: map[string]ServerConfig{"peeq": {Transport: TransportStreamableHTTP, URL: server.URL}}}

	service, err := NewServiceFromConfigs(context.Background(), Config{}, bestEffort, server.Client(), nil)
	if err != nil {
		t.Fatalf("NewServiceFromConfigs() error: %v", err)
	}
	if len(service.Tools()) != 0 {
		t.Fatalf("tools while down = %d, want 0", len(service.Tools()))
	}
	if got := service.nextRefreshDelay(); got != downServerRetryInterval {
		t.Fatalf("first delay with a missing server = %v, want %v", got, downServerRetryInterval)
	}

	up.Store(true)
	if next := service.Refresh(context.Background(), nil); next != 2*time.Minute {
		t.Fatalf("Refresh() next = %v, want the server's 2m ttl", next)
	}
	if !service.HasTool("peeq__search") || service.ServerInstructions("peeq") != "hint for search" {
		t.Fatalf("after recovery: tools=%v instructions=%q", service.Tools(), service.ServerInstructions("peeq"))
	}

	name.Store("find")
	service.Refresh(context.Background(), nil)
	if service.HasTool("peeq__search") || !service.HasTool("peeq__find") {
		t.Fatalf("after rename: tools=%v", service.Tools())
	}

	up.Store(false)
	if next := service.Refresh(context.Background(), nil); next != downServerRetryInterval {
		t.Fatalf("Refresh() while down next = %v", next)
	}
	if !service.HasTool("peeq__find") {
		t.Fatal("a failed refresh dropped the server's last tools")
	}
}

func TestToolAllowedHonoursUsersAllowlist(t *testing.T) {
	service := &Service{cfg: Config{Servers: map[string]ServerConfig{
		"peeq": {RestrictUsers: true, Users: []string{"jan"}},
		"none": {RestrictUsers: true},
		"open": {},
	}}}
	cases := []struct {
		user, tool string
		want       bool
	}{
		{"jan", "peeq__search", true},
		{"JAN", "peeq__search", true},
		{"eve", "peeq__search", false},
		{"", "peeq__search", false},
		{"jan", "none__x", false},
		{"eve", "open__x", true},
		{"eve", "unprefixed", true},
	}
	for _, c := range cases {
		if got := service.ToolAllowed(c.user, c.tool); got != c.want {
			t.Errorf("ToolAllowed(%q, %q) = %v, want %v", c.user, c.tool, got, c.want)
		}
	}
}

func TestCapInstructionsKeepsRuneBoundary(t *testing.T) {
	long := ""
	for len(long) < maxServerInstructionsBytes+10 {
		long += "ä"
	}
	got := capInstructions(long)
	if len(got) > maxServerInstructionsBytes || got[len(got)-2:] != "ä" {
		t.Fatalf("capInstructions cut badly: len=%d tail=%q", len(got), got[len(got)-2:])
	}
}
