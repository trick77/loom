package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"testing"
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

var _ = exec.Command
