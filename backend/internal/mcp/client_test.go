package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRemoteClientListsAndCallsTools(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("X-Test") != "yes" {
			t.Fatalf("X-Test header = %q, want yes", r.Header.Get("X-Test"))
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		methods = append(methods, req.Method)
		switch req.Method {
		case "initialize":
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "tools/list":
			writeRPCResult(t, w, req.ID, map[string]any{
				"tools": []map[string]any{{
					"name":        "search",
					"description": "Search the web",
					"inputSchema": map[string]any{"type": "object"},
				}},
			})
		case "tools/call":
			writeRPCResult(t, w, req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "result text"}},
			})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	t.Cleanup(server.Close)

	client := NewRemoteClient("search", ServerConfig{
		Transport: TransportStreamableHTTP,
		URL:       server.URL,
		Headers:   map[string]string{"X-Test": "yes"},
	}, server.Client())

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "search__search" || tools[0].OriginalName != "search" {
		t.Fatalf("tools = %#v", tools)
	}
	got, err := client.CallTool(context.Background(), "search", map[string]any{"q": "spark"})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if got != "result text" {
		t.Fatalf("CallTool() = %q, want result text", got)
	}
	if !reflect.DeepEqual(methods, []string{"initialize", "tools/list", "tools/call"}) {
		t.Fatalf("methods = %#v", methods)
	}
}

// TestRemoteClientHandlesStreamableHTTPSessionAndSSE mirrors a spec-compliant
// Streamable HTTP server (e.g. the mcp-searxng image): it rejects requests that
// do not accept text/event-stream (406), hands out a session id on initialize
// that every later request must echo back (400 otherwise), and replies with SSE
// framing rather than a bare JSON body.
func TestRemoteClientHandlesStreamableHTTPSessionAndSSE(t *testing.T) {
	const sessionID = "sess-123"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		if !strings.Contains(accept, "application/json") || !strings.Contains(accept, "text/event-stream") {
			http.Error(w, `{"error":"must accept both application/json and text/event-stream"}`, http.StatusNotAcceptable)
			return
		}
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		if req.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != sessionID {
			http.Error(w, `{"error":"No valid session ID provided"}`, http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", sessionID)
			writeSSEResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "tools/list":
			writeSSEResult(t, w, req.ID, map[string]any{
				"tools": []map[string]any{{
					"name":        "web_search",
					"description": "Search the web",
					"inputSchema": map[string]any{"type": "object"},
				}},
			})
		case "tools/call":
			writeSSEResult(t, w, req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "result text"}},
			})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	t.Cleanup(server.Close)

	client := NewRemoteClient("search", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL}, server.Client())

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "search__web_search" {
		t.Fatalf("tools = %#v", tools)
	}
	got, err := client.CallTool(context.Background(), "web_search", map[string]any{"query": "spark"})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if got != "result text" {
		t.Fatalf("CallTool() = %q, want result text", got)
	}
}

func TestRemoteClientRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":`))
			_, _ = w.Write([]byte("2"))
			_, _ = w.Write([]byte(`,"result":{"tools":[{"name":"huge","description":"`))
			_, _ = w.Write([]byte(strings.Repeat("x", maxRPCResponseBytes)))
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	t.Cleanup(server.Close)

	client := NewRemoteClient("search", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL}, server.Client())

	if _, err := client.ListTools(context.Background()); err == nil {
		t.Fatal("ListTools() error = nil, want oversized response error")
	}
}

func TestStdioClientListsTools(t *testing.T) {
	if os.Getenv("SPARK_MCP_TEST_HELPER") == "1" {
		runMCPTestHelper(t)
		return
	}

	client := NewStdioClient("local", ServerConfig{
		Transport: TransportStdio,
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestStdioClientListsTools"},
		Env:       map[string]string{"SPARK_MCP_TEST_HELPER": "1"},
	})
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "local__echo" {
		t.Fatalf("tools = %#v", tools)
	}
	got, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if got != "echo result" {
		t.Fatalf("CallTool() = %q, want echo result", got)
	}
}

func writeSSEResult(t *testing.T, w http.ResponseWriter, id int64, result any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if err != nil {
		t.Fatalf("Marshal SSE payload: %v", err)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	if _, err := w.Write([]byte("event: message\ndata: " + string(payload) + "\n\n")); err != nil {
		t.Fatalf("write SSE: %v", err)
	}
}

func writeRPCResult(t *testing.T, w http.ResponseWriter, id int64, result any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}); err != nil {
		t.Fatalf("Encode response: %v", err)
	}
}

func runMCPTestHelper(t *testing.T) {
	t.Helper()
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			t.Fatalf("helper decode: %v", err)
		}
		switch req.Method {
		case "initialize":
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"protocolVersion": "2025-06-18"}})
		case "tools/list":
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"tools": []map[string]any{{"name": "echo", "description": "Echo", "inputSchema": map[string]any{"type": "object"}}},
			}})
		case "tools/call":
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "echo result"}},
			}})
		}
	}
}

func TestStdioClientCommandFailure(t *testing.T) {
	client := NewStdioClient("bad", ServerConfig{Transport: TransportStdio, Command: "definitely-missing-spark-mcp-helper"})
	defer client.Close()

	if _, err := client.ListTools(context.Background()); err == nil {
		t.Fatal("ListTools() error = nil, want command failure")
	}
}

func TestStdioClientCallHonorsContextAndCloseDoesNotDeadlock(t *testing.T) {
	if os.Getenv("SPARK_MCP_HANG_HELPER") == "1" {
		time.Sleep(10 * time.Second)
		return
	}
	client := NewStdioClient("hang", ServerConfig{
		Transport: TransportStdio,
		Command:   os.Args[0],
		Args:      []string{"-test.run=TestStdioClientCallHonorsContextAndCloseDoesNotDeadlock"},
		Env:       map[string]string{"SPARK_MCP_HANG_HELPER": "1"},
	})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.ListTools(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ListTools() error = %v, want context deadline exceeded", err)
	}

	done := make(chan struct{})
	go func() {
		_ = client.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close() did not return")
	}
}

var _ = exec.Command
