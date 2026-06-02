package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearxngClientListsWebSearchTool(t *testing.T) {
	client := NewSearxngClient("searxng", "http://searxng:8080", nil)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ListTools() len = %d, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != "searxng__web_search" || tool.OriginalName != "web_search" || tool.ServerName != "searxng" {
		t.Fatalf("tool identity = %#v", tool)
	}
	required, ok := tool.InputSchema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "q" {
		t.Fatalf("required schema = %#v, want [q]", tool.InputSchema["required"])
	}
}

func TestSearxngClientSearchesAndFormatsResults(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.URL.Path != "/search" {
			t.Fatalf("path = %q, want /search", r.URL.Path)
		}
		writeSearxngJSON(t, w, map[string]any{
			"results": []map[string]any{
				{
					"title":   "Spark project",
					"url":     "https://example.com/spark",
					"content": "A self-hosted chat app.",
					"engine":  "duckduckgo",
				},
				{
					"title": "Spark docs",
					"url":   "https://example.com/docs",
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	client := NewSearxngClient("searxng", server.URL, server.Client())
	got, err := client.CallTool(context.Background(), "web_search", map[string]any{
		"q":           "spark chat",
		"categories":  "general",
		"language":    "en",
		"pageno":      2,
		"safesearch":  1,
		"time_range":  "month",
		"max_results": 2,
	})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}

	for _, want := range []string{
		"1. Spark project",
		"URL: https://example.com/spark",
		"Snippet: A self-hosted chat app.",
		"Source: duckduckgo",
		"2. Spark docs",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("CallTool() missing %q in:\n%s", want, got)
		}
	}
	for _, want := range []string{
		"format=json",
		"q=spark+chat",
		"categories=general",
		"language=en",
		"pageno=2",
		"safesearch=1",
		"time_range=month",
	} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestSearxngClientReturnsNoResultsMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSearxngJSON(t, w, map[string]any{"results": []map[string]any{}})
	}))
	t.Cleanup(server.Close)

	client := NewSearxngClient("searxng", server.URL, server.Client())
	got, err := client.CallTool(context.Background(), "web_search", map[string]any{"q": "nothing"})
	if err != nil {
		t.Fatalf("CallTool() error: %v", err)
	}
	if got != `No SearXNG results for "nothing".` {
		t.Fatalf("CallTool() = %q", got)
	}
}

func TestSearxngClientRejectsBadInputsAndResponses(t *testing.T) {
	client := NewSearxngClient("searxng", "http://127.0.0.1:1", nil)
	if _, err := client.CallTool(context.Background(), "unknown", map[string]any{"q": "spark"}); err == nil {
		t.Fatal("CallTool() unknown tool error = nil")
	}
	if _, err := client.CallTool(context.Background(), "web_search", map[string]any{}); err == nil {
		t.Fatal("CallTool() missing q error = nil")
	}
	if _, err := client.CallTool(context.Background(), "web_search", map[string]any{"q": "spark", "pageno": 0}); err == nil {
		t.Fatal("CallTool() invalid pageno error = nil")
	}
	if _, err := client.CallTool(context.Background(), "web_search", map[string]any{"q": "spark", "safesearch": 9}); err == nil {
		t.Fatal("CallTool() invalid safesearch error = nil")
	}

	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad upstream", http.StatusBadGateway)
	}))
	t.Cleanup(badStatus.Close)
	if _, err := NewSearxngClient("searxng", badStatus.URL, badStatus.Client()).CallTool(context.Background(), "web_search", map[string]any{"q": "spark"}); err == nil {
		t.Fatal("CallTool() bad status error = nil")
	}

	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	t.Cleanup(badJSON.Close)
	if _, err := NewSearxngClient("searxng", badJSON.URL, badJSON.Client()).CallTool(context.Background(), "web_search", map[string]any{"q": "spark"}); err == nil {
		t.Fatal("CallTool() bad JSON error = nil")
	}
}

func writeSearxngJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode response: %v", err)
	}
}
