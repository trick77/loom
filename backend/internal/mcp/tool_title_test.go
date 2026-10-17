package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServiceToolTitleFromToolsList(t *testing.T) {
	server, _ := newModernServer(t, nil)
	// newModernServer lists one untitled tool; titles come from a second server.
	titled := newTitledServer(t)
	clients := map[string]Client{
		"plain": NewRemoteClient("plain", ServerConfig{Transport: TransportStreamableHTTP, URL: server.URL}, server.Client()),
		"peeq":  NewRemoteClient("peeq", ServerConfig{Transport: TransportStreamableHTTP, URL: titled.URL}, titled.Client()),
	}
	service, err := NewRequiredServiceFromClients(context.Background(), clients)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"peeq__search":    "Search your video library",
		"peeq__get_video": "Get a video's summaries",
		"plain__search":   "",
		"missing__tool":   "",
	}
	for name, want := range cases {
		if got := service.ToolTitle(name); got != want {
			t.Errorf("ToolTitle(%q) = %q, want %q", name, got, want)
		}
	}
}

// newTitledServer is a modern server whose tools carry display titles, one as
// the top-level title and one only as annotations.title.
func newTitledServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req modernRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		result := map[string]any{"resultType": "complete"}
		if req.Method == "tools/list" {
			result["tools"] = []map[string]any{
				{"name": "search", "title": "Search your video library", "inputSchema": map[string]any{"type": "object"}},
				{"name": "get_video", "annotations": map[string]any{"title": "Get a video's summaries"}, "inputSchema": map[string]any{"type": "object"}},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(server.Close)
	return server
}

func TestToolResultDisplayTitlePrefersTopLevel(t *testing.T) {
	var tool toolResult
	_ = json.Unmarshal([]byte(`{"name":"x","title":" Top ","annotations":{"title":"Annotated"}}`), &tool)
	if got := tool.displayTitle(); got != "Top" {
		t.Fatalf("displayTitle() = %q, want Top", got)
	}
	_ = json.Unmarshal([]byte(`{"name":"x","annotations":{"title":"Annotated"}}`), &tool)
	tool.Title = ""
	if got := tool.displayTitle(); got != "Annotated" {
		t.Fatalf("displayTitle() fallback = %q, want Annotated", got)
	}
}
