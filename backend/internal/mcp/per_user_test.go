package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/trick77/loom/internal/loomuser"
)

// perUserServer answers a modern MCP server and records the user header of
// every tools/call.
func perUserServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var users []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode: %v", err)
		}
		switch req.Method {
		case "server/discover":
			writeRPCResult(t, w, req.ID, map[string]any{})
		case "tools/list":
			if r.Header.Get(loomuser.Header) != "" {
				t.Errorf("tools/list carries a user: %q", r.Header.Get(loomuser.Header))
			}
			writeRPCResult(t, w, req.ID, map[string]any{"tools": []map[string]any{{"name": "mine", "inputSchema": map[string]any{"type": "object"}}}})
		case "tools/call":
			mu.Lock()
			users = append(users, r.Header.Get(loomuser.Header))
			mu.Unlock()
			writeRPCResult(t, w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), users...)
	}
}

func TestRemoteClientPerUserSendsOnlyTheCaller(t *testing.T) {
	server, seen := perUserServer(t)
	client := NewRemoteClient("lib", ServerConfig{
		Transport: TransportStreamableHTTP, URL: server.URL, PerUser: true,
		// A configured header can never name the user.
		Headers: map[string]string{loomuser.Header: "spoofed"},
	}, server.Client())
	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, id := range []string{"alice", "bob"} {
		if _, err := client.CallTool(WithPrincipal(context.Background(), Principal{ID: id}), "mine", nil); err != nil {
			t.Fatalf("CallTool(%s): %v", id, err)
		}
	}
	if got := seen(); len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Fatalf("users seen = %v", got)
	}
}

func TestRemoteClientPerUserRefusesCallWithoutCaller(t *testing.T) {
	server, seen := perUserServer(t)
	client := NewRemoteClient("lib", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL, PerUser: true}, server.Client())
	for _, ctx := range []context.Context{context.Background(), WithPrincipal(context.Background(), Principal{})} {
		_, err := client.CallTool(ctx, "mine", nil)
		if err == nil || !strings.Contains(err.Error(), "needs the calling user") {
			t.Fatalf("err = %v", err)
		}
	}
	if got := seen(); len(got) != 0 {
		t.Fatalf("a call reached the server: %v", got)
	}
}

func TestRemoteClientSharedServerNeverGetsUser(t *testing.T) {
	server, seen := perUserServer(t)
	client := NewRemoteClient("search", ServerConfig{
		Transport: TransportStreamableHTTP, URL: server.URL,
		Headers: map[string]string{loomuser.Header: "spoofed"},
	}, server.Client())
	if _, err := client.CallTool(WithPrincipal(context.Background(), Principal{ID: "alice"}), "mine", nil); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := seen(); len(got) != 1 || got[0] != "" {
		t.Fatalf("a shared server learned a loom id: %v", got)
	}
}

// The service refuses a per-user call without a caller, and a per-user server
// whose override cannot name the caller (a stdio entry), whatever the client.
func TestServicePerUserGuard(t *testing.T) {
	server, seen := perUserServer(t)
	cfg := Config{Servers: map[string]ServerConfig{
		"lib": {Transport: TransportStreamableHTTP, URL: server.URL, PerUser: true, RestrictUsers: true, Users: []string{"alice"}},
	}}
	svc, err := NewServiceFromConfigs(context.Background(), Config{}, cfg, server.Client(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CallToolResult(context.Background(), "lib__mine", nil); err == nil || !strings.Contains(err.Error(), "needs the calling user") {
		t.Fatalf("no caller: %v", err)
	}
	if _, err := svc.CallToolResult(WithPrincipal(context.Background(), Principal{ID: "alice"}), "lib__mine", nil); err != nil {
		t.Fatalf("alice: %v", err)
	}
	if got := seen(); len(got) != 1 || got[0] != "alice" {
		t.Fatalf("users seen = %v", got)
	}
	stdio := ServerConfig{Transport: TransportStdio, PerUser: true}
	if err := perUserReady(WithPrincipal(context.Background(), Principal{ID: "alice"}), stdio, "x__y"); err == nil {
		t.Error("a stdio per-user server was allowed: it cannot name the caller")
	}
	if err := perUserReady(context.Background(), ServerConfig{Transport: TransportStdio}, "x__y"); err != nil {
		t.Errorf("a shared server needs no caller: %v", err)
	}
}

func TestPrincipalFrom(t *testing.T) {
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Error("empty context has a principal")
	}
	if p, ok := PrincipalFrom(WithPrincipal(context.Background(), Principal{ID: "x"})); !ok || p.ID != "x" {
		t.Errorf("got %+v %v", p, ok)
	}
}

func TestTunewireLibraryIsPerUser(t *testing.T) {
	if !TunewireLibraryServerConfig("http://t", "tok", []string{"a"}).PerUser {
		t.Error("tunewire library must keep one account per user")
	}
	if TunewireCatalogServerConfig("http://t", "tok").PerUser {
		t.Error("catalog search reads no one's data")
	}
}
