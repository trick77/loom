package turntest

import (
	"context"
	"errors"

	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

// ErrTool is the failure a fake tool call returns.
var ErrTool = errors.New("fake tool failed")

// ToolService is a tool service exposing ToolList; CallTool returns Result and
// Err unless CallFunc is set.
type ToolService struct {
	ToolList  []llm.Tool
	Result    string
	Err       error
	Available map[string]bool
	Servers   []mcp.ServerStatus
	CallFunc  func(ctx context.Context, name string, args map[string]any) (string, error)
	// ToolCategories optionally tags an exposed tool name with the categories its
	// server is relevant to, mirroring mcp.Service.ToolsFor. A tool absent here (or
	// with an empty list) is category-neutral and always returned.
	ToolCategories map[string][]string
}

// Tools implements turn.ToolService.
func (f ToolService) Tools() []llm.Tool {
	return f.ToolList
}

// ToolsFor mirrors mcp.Service.ToolsFor semantics: a category-neutral tool (none
// registered in toolCategories) is always returned; a category-tagged tool is
// returned only when the active set contains one of its categories. With no
// toolCategories configured this returns the full list, preserving the behavior
// tests that predate category gating rely on.
func (f ToolService) ToolsFor(active map[string]bool) []llm.Tool {
	if len(f.ToolCategories) == 0 {
		return f.ToolList
	}
	out := make([]llm.Tool, 0, len(f.ToolList))
	for _, tool := range f.ToolList {
		cats := f.ToolCategories[tool.Function.Name]
		if len(cats) == 0 {
			out = append(out, tool)
			continue
		}
		for _, c := range cats {
			if active[c] {
				out = append(out, tool)
				break
			}
		}
	}
	return out
}

// ServerStatus implements turn.ToolService.
func (f ToolService) ServerStatus(context.Context) []mcp.ServerStatus {
	return f.Servers
}

// CallTool implements turn.ToolService.
func (f ToolService) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if f.CallFunc != nil {
		return f.CallFunc(ctx, name, args)
	}
	if f.Err != nil {
		return "", f.Err
	}
	return f.Result, nil
}

// HasTool implements turn.ToolService.
func (f ToolService) HasTool(name string) bool {
	return f.Available[name]
}
