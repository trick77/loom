package mcp

import (
	"context"
	"testing"

	"github.com/trick77/spark/internal/llm"
)

func TestServiceMapsToolsAndRoutesCalls(t *testing.T) {
	service, err := NewService(map[string]Client{
		"search": fakeClient{
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
		"a": fakeClient{tools: []Tool{{Name: "dup__tool", OriginalName: "tool", ServerName: "dup"}}},
		"b": fakeClient{tools: []Tool{{Name: "dup__tool", OriginalName: "tool", ServerName: "dup"}}},
	})
	if err == nil {
		t.Fatal("NewService() error = nil, want duplicate error")
	}
}

type fakeClient struct {
	tools  []Tool
	result string
}

func (f fakeClient) ListTools(context.Context) ([]Tool, error) {
	return f.tools, nil
}

func (f fakeClient) CallTool(context.Context, string, map[string]any) (string, error) {
	return f.result, nil
}

func (f fakeClient) Close() error { return nil }

var _ = llm.Tool{}
