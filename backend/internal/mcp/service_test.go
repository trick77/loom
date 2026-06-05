package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/trick77/spark/internal/llm"
)

func TestServiceMapsToolsAndRoutesCalls(t *testing.T) {
	service, err := NewService(map[string]Client{
		"search": &fakeClient{
			tools: []Tool{{
				Name:         "search__web",
				OriginalName: "web",
				Description:  "Search",
				InputSchema:  map[string]any{"type": "object"},
				ServerName:   "search",
			}},
			result: "ok",
		},
	})
	if err != nil {
		t.Fatalf("NewService() error: %v", err)
	}

	tools := service.Tools()
	if len(tools) != 1 || tools[0].Function.Name != "search__web" {
		t.Fatalf("tools = %#v", tools)
	}
	got, err := service.CallTool(context.Background(), "search__web", map[string]any{"q": "spark"})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("CallTool() = %q, want ok", got)
	}
}

func TestServiceRejectsDuplicateToolNames(t *testing.T) {
	_, err := NewService(map[string]Client{
		"a": &fakeClient{tools: []Tool{{Name: "dup__tool", OriginalName: "tool", ServerName: "dup"}}},
		"b": &fakeClient{tools: []Tool{{Name: "dup__tool", OriginalName: "tool", ServerName: "dup"}}},
	})
	if err == nil {
		t.Fatal("NewService() error = nil, want duplicate error")
	}
}

func TestServiceFromConfigFailsWhenConfiguredServerIsUnavailable(t *testing.T) {
	cfg := Config{Servers: map[string]ServerConfig{
		"fetch": {Transport: TransportStreamableHTTP, URL: "http://127.0.0.1:1/mcp"},
	}}

	_, err := NewRequiredServiceFromConfig(context.Background(), cfg, http.DefaultClient)
	if err == nil {
		t.Fatal("NewRequiredServiceFromConfig() error = nil, want unavailable server error")
	}
	if !strings.Contains(err.Error(), "list MCP tools for fetch") {
		t.Fatalf("NewRequiredServiceFromConfig() error = %q, want fetch discovery context", err)
	}
}

func TestServiceFromConfigClosesUnavailableClient(t *testing.T) {
	client := &fakeClient{err: errors.New("down")}
	_, err := NewRequiredServiceFromClients(context.Background(), map[string]Client{
		"fetch": client,
	})
	if err == nil {
		t.Fatal("NewRequiredServiceFromClients() error = nil, want discovery error")
	}
	if !client.closed {
		t.Fatal("unavailable client was not closed")
	}
}

func TestServiceServerStatusReportsReachableAndUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "tools/list":
			writeRPCResult(t, w, req.ID, map[string]any{"tools": []map[string]any{}})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	t.Cleanup(server.Close)

	cfg := Config{Servers: map[string]ServerConfig{
		"alpha": {Transport: TransportStreamableHTTP, URL: server.URL},
		"zeta":  {Transport: TransportStreamableHTTP, URL: "http://127.0.0.1:1"},
	}}
	service, err := NewBestEffortServiceFromConfig(context.Background(), cfg, server.Client(), nil)
	if err != nil {
		t.Fatalf("NewBestEffortServiceFromConfig() error: %v", err)
	}

	statuses := service.ServerStatus(context.Background())
	if len(statuses) != 2 {
		t.Fatalf("ServerStatus() len = %d, want 2: %#v", len(statuses), statuses)
	}
	if statuses[0].Name != "alpha" || statuses[1].Name != "zeta" {
		t.Fatalf("ServerStatus() not sorted by name: %#v", statuses)
	}
	active := map[string]bool{}
	for _, st := range statuses {
		active[st.Name] = st.Active
	}
	if !active["alpha"] {
		t.Errorf("alpha Active = false, want true")
	}
	if active["zeta"] {
		t.Errorf("zeta Active = true, want false")
	}
}

// tavilyMockServer mimics Tavily's hosted MCP endpoint: it answers the MCP
// JSON-RPC handshake and advertises several tools, of which only tavily_search
// must survive the ServerConfig.Tools allowlist.
func tavilyMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "tools/list":
			writeRPCResult(t, w, req.ID, map[string]any{"tools": []map[string]any{
				{"name": "tavily_search", "description": "Web search", "inputSchema": map[string]any{"type": "object"}},
				{"name": "tavily_extract", "description": "Extract", "inputSchema": map[string]any{"type": "object"}},
				{"name": "tavily_crawl", "description": "Crawl", "inputSchema": map[string]any{"type": "object"}},
			}})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
}

func TestBestEffortServiceIncludesSyntheticTavilyAndExternalTools(t *testing.T) {
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "tools/list":
			writeRPCResult(t, w, req.ID, map[string]any{"tools": []map[string]any{{
				"name":        "fetch",
				"description": "Fetch URL",
				"inputSchema": map[string]any{"type": "object"},
			}}})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	t.Cleanup(external.Close)

	tavily := tavilyMockServer(t)
	t.Cleanup(tavily.Close)

	cfg := Config{Servers: map[string]ServerConfig{
		"fetch":  {Transport: TransportStreamableHTTP, URL: external.URL},
		"tavily": TavilyServerConfig(tavily.URL, "test-key"),
	}}
	service, err := NewBestEffortServiceFromConfig(context.Background(), cfg, external.Client(), nil)
	if err != nil {
		t.Fatalf("NewBestEffortServiceFromConfig() error: %v", err)
	}

	names := []string{}
	for _, tool := range service.Tools() {
		names = append(names, tool.Function.Name)
	}
	// The Tools allowlist keeps only tavily_search despite the mock advertising
	// extract/crawl too.
	if !reflect.DeepEqual(names, []string{"fetch__fetch", "tavily__tavily_search"}) {
		t.Fatalf("tool names = %#v", names)
	}
}

func TestServiceServerStatusProbesSyntheticTavilyConfig(t *testing.T) {
	tavily := tavilyMockServer(t)
	t.Cleanup(tavily.Close)

	cfg := Config{Servers: map[string]ServerConfig{
		"tavily": TavilyServerConfig(tavily.URL, "test-key"),
	}}
	service, err := NewBestEffortServiceFromConfig(context.Background(), cfg, tavily.Client(), nil)
	if err != nil {
		t.Fatalf("NewBestEffortServiceFromConfig() error: %v", err)
	}

	statuses := service.ServerStatus(context.Background())
	if len(statuses) != 1 || statuses[0].Name != "tavily" || !statuses[0].Active {
		t.Fatalf("ServerStatus() = %#v, want active tavily", statuses)
	}
}

func TestServiceServerStatusNilAndEmpty(t *testing.T) {
	var nilService *Service
	if got := nilService.ServerStatus(context.Background()); got != nil {
		t.Errorf("nil service ServerStatus() = %#v, want nil", got)
	}
	empty, err := NewBestEffortServiceFromConfig(context.Background(), Config{Servers: map[string]ServerConfig{}}, nil, nil)
	if err != nil {
		t.Fatalf("NewBestEffortServiceFromConfig() error: %v", err)
	}
	if got := empty.ServerStatus(context.Background()); got != nil {
		t.Errorf("empty service ServerStatus() = %#v, want nil", got)
	}
}

type fakeClient struct {
	tools  []Tool
	result string
	err    error
	closed bool
}

func (f *fakeClient) ListTools(context.Context) ([]Tool, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.tools, nil
}

func (f *fakeClient) CallTool(context.Context, string, map[string]any) (string, error) {
	return f.result, nil
}

func (f *fakeClient) Close() error {
	f.closed = true
	return nil
}

var _ = llm.Tool{}
