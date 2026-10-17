package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"
)

type modernRequest struct {
	ID     int64          `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// newModernServer is a 2026-07-28 server: it validates the mirrored headers
// and _meta the way the spec requires and refuses initialize like a
// modern-only server does.
func newModernServer(t *testing.T, onCall func(req modernRequest) any) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	methods := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req modernRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		mu.Lock()
		methods = append(methods, req.Method)
		mu.Unlock()
		if r.Header.Get("Mcp-Session-Id") != "" {
			t.Errorf("modern request carried a session id")
		}
		meta, _ := req.Params["_meta"].(map[string]any)
		if r.Header.Get("MCP-Protocol-Version") != modernProtocolVersion || meta[metaProtocolVersion] != modernProtocolVersion || r.Header.Get("Mcp-Method") != req.Method {
			w.WriteHeader(http.StatusBadRequest)
			writeRPCError(w, req.ID, codeHeaderMismatch, "header mismatch")
			return
		}
		if req.Method == "tools/call" && r.Header.Get("Mcp-Name") != req.Params["name"] {
			w.WriteHeader(http.StatusBadRequest)
			writeRPCError(w, req.ID, codeHeaderMismatch, "Mcp-Name mismatch")
			return
		}
		var result any
		switch req.Method {
		case "server/discover":
			result = map[string]any{"resultType": "complete", "supportedVersions": []string{modernProtocolVersion}, "capabilities": map[string]any{"tools": map[string]any{}}, "instructions": "search me for videos"}
		case "tools/list":
			result = map[string]any{"resultType": "complete", "ttlMs": 300000, "cacheScope": "private", "tools": []map[string]any{{"name": "search", "description": "Search", "inputSchema": map[string]any{"type": "object"}}}}
		default:
			if onCall == nil {
				w.WriteHeader(http.StatusNotFound)
				writeRPCError(w, req.ID, codeMethodNotFound, "method not found")
				return
			}
			result = onCall(req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return server, &methods
}

func writeRPCError(w http.ResponseWriter, id int64, code int, message string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func TestRemoteClientSpeaksModernProtocol(t *testing.T) {
	server, methods := newModernServer(t, func(modernRequest) any {
		return map[string]any{
			"resultType": "complete",
			"content":    []map[string]any{{"type": "text", "text": "1. A video"}},
			"structuredContent": map[string]any{"sources": []map[string]any{
				{"url": "https://peeq.example/?v=abc&t=42", "title": "A video", "label": "peeq · Chan", "snippet": "about things"},
				{"url": "  ", "title": "dropped: no url"},
			}},
		}
	})
	client := NewRemoteClient("peeq", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL}, server.Client())

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "peeq__search" {
		t.Fatalf("tools = %#v", tools)
	}
	meta := client.(serverMetadata)
	if got := meta.Instructions(); got != "search me for videos" {
		t.Fatalf("Instructions() = %q", got)
	}
	if got := meta.ToolListTTL(); got != 5*time.Minute {
		t.Fatalf("ToolListTTL() = %v, want 5m from ttlMs", got)
	}
	got, err := client.CallTool(context.Background(), "search", map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	// The URL-less entry keeps its slot: sources[i] is the text's item i+1.
	want := []Source{{URL: "https://peeq.example/?v=abc&t=42", Title: "A video", Label: "peeq · Chan", Snippet: "about things"}, {Title: "dropped: no url"}}
	if got.Text != "1. A video" || !reflect.DeepEqual(got.Sources, want) {
		t.Fatalf("CallTool() = %#v", got)
	}
	if !reflect.DeepEqual(*methods, []string{"server/discover", "tools/list", "tools/call"}) {
		t.Fatalf("methods = %#v, want no initialize", *methods)
	}
}

// A modern server's rejection must surface, never send the client into a
// legacy handshake the server does not speak.
func TestRemoteClientDoesNotFallBackOnModernError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req modernRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "initialize" {
			t.Errorf("client fell back to initialize after a modern error")
		}
		w.WriteHeader(http.StatusBadRequest)
		writeRPCError(w, req.ID, codeHeaderMismatch, "header mismatch")
	}))
	t.Cleanup(server.Close)
	client := NewRemoteClient("m", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL}, server.Client())
	if _, err := client.ListTools(context.Background()); err == nil {
		t.Fatal("ListTools() error = nil, want the header mismatch")
	}
}

// A legacy server that answers the unknown probe with a 500 still works
// through the handshake: discovery must not fail over a method it never knew.
func TestRemoteClientFallsBackWhenProbeFails5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "server/discover":
			http.Error(w, "internal error", http.StatusInternalServerError)
		case "initialize":
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18", "instructions": "legacy hint"})
		case "tools/list":
			writeRPCResult(t, w, req.ID, map[string]any{"tools": []map[string]any{{"name": "t", "inputSchema": map[string]any{"type": "object"}}}})
		}
	}))
	t.Cleanup(server.Close)
	client := NewRemoteClient("l", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL}, server.Client())
	tools, err := client.ListTools(context.Background())
	if err != nil || len(tools) != 1 {
		t.Fatalf("ListTools() = %v, %v", tools, err)
	}
	meta := client.(serverMetadata)
	if meta.Instructions() != "legacy hint" || meta.ToolListTTL() != defaultToolListTTL {
		t.Fatalf("metadata = %q, %v", meta.Instructions(), meta.ToolListTTL())
	}
}

// A dual-era server that only accepts an older revision says so with -32022;
// listing a legacy revision loom speaks means the handshake.
func TestRemoteClientFallsBackOnUnsupportedVersionListingLegacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "server/discover":
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{
				"code": codeUnsupportedProtocolVersion, "message": "Unsupported protocol version",
				"data": map[string]any{"supported": []string{"2025-11-25"}, "requested": modernProtocolVersion},
			}})
		case "initialize":
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "tools/list":
			writeRPCResult(t, w, req.ID, map[string]any{"tools": []map[string]any{}})
		}
	}))
	t.Cleanup(server.Close)
	client := NewRemoteClient("d", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL}, server.Client())
	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
}

// A legacy server answering server/discover with a JSON-RPC MethodNotFound is
// still legacy: a modern server must implement discover.
func TestRemoteClientFallsBackOnMethodNotFoundProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Method {
		case "server/discover":
			w.WriteHeader(http.StatusNotFound)
			writeRPCError(w, req.ID, codeMethodNotFound, "method not found")
		case "initialize":
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "tools/list":
			writeRPCResult(t, w, req.ID, map[string]any{"tools": []map[string]any{{"name": "t", "inputSchema": map[string]any{"type": "object"}}}})
		}
	}))
	t.Cleanup(server.Close)
	client := NewRemoteClient("l", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL}, server.Client())
	if tools, err := client.ListTools(context.Background()); err != nil || len(tools) != 1 {
		t.Fatalf("ListTools() = %v, %v", tools, err)
	}
}

func TestHeaderSafeValue(t *testing.T) {
	cases := map[string]string{
		"search":         "search",
		"Hello, 世界":      "=?base64?SGVsbG8sIOS4lueVjA==?=",
		" padded ":       "=?base64?IHBhZGRlZCA=?=",
		"=?base64?abc?=": "=?base64?PT9iYXNlNjQ/YWJjPz0=?=",
		"line1\nline2":   "=?base64?bGluZTEKbGluZTI=?=",
	}
	for in, want := range cases {
		if got := headerSafeValue(in); got != want {
			t.Errorf("headerSafeValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToolListTTLClamps(t *testing.T) {
	tiny, zero := int64(10), int64(0)
	if got := toolListTTL(&tiny); got != minToolListTTL {
		t.Fatalf("tiny ttl = %v", got)
	}
	if got := toolListTTL(&zero); got != defaultToolListTTL {
		t.Fatalf("zero ttl = %v", got)
	}
	if got := toolListTTL(nil); got != defaultToolListTTL {
		t.Fatalf("nil ttl = %v", got)
	}
}

func TestStructuredSourcesIgnoresOtherShapes(t *testing.T) {
	for _, raw := range []string{``, `null`, `"text"`, `[1,2]`, `{"sources":"nope"}`, `{"other":1}`, `{"sources":[{"url":" "}]}`} {
		if got := structuredSources(json.RawMessage(raw)); got != nil {
			t.Errorf("structuredSources(%s) = %#v, want nil", raw, got)
		}
	}
}
