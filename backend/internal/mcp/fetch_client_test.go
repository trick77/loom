package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestFetchClientAdvertisesFetchTool(t *testing.T) {
	client := NewFetchClient("fetch")
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools error: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ListTools returned %d tools, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != "fetch__fetch" {
		t.Fatalf("exposed name = %q, want fetch__fetch", tool.Name)
	}
	if tool.OriginalName != "fetch" {
		t.Fatalf("original name = %q, want fetch", tool.OriginalName)
	}
	props, ok := tool.InputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema properties missing: %#v", tool.InputSchema)
	}
	for _, key := range []string{"url", "max_length", "start_index", "raw", "extract_pdf", "include_metadata", "full_page", "selector", "exclude_selectors"} {
		if _, ok := props[key]; !ok {
			t.Errorf("InputSchema missing property %q", key)
		}
	}
}

func TestFetchClientCallToolRequiresURL(t *testing.T) {
	client := NewFetchClient("fetch")
	// An empty URL fails before any network access, and the error must be
	// non-nil so the deterministic fetch->obscura fallback fires.
	_, err := client.CallTool(context.Background(), "fetch", map[string]any{})
	if err == nil {
		t.Fatal("CallTool with no url should error")
	}
	if !strings.Contains(err.Error(), "URL is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFetchClientCloseIsNil(t *testing.T) {
	if err := NewFetchClient("fetch").Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}
