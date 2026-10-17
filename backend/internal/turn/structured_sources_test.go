package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
	"github.com/trick77/loom/internal/mcp"
)

func TestRelabelStructuredSourcesMapsResultsToRegistryMarkers(t *testing.T) {
	reg := newWebSourceRegistryAfter(2)
	sources := []mcp.Source{
		{URL: "", Title: "no link"},
		{URL: "https://peeq.example/?v=a&t=10", Title: "First", Label: "peeq · Chan", Snippet: "one"},
		{URL: "https://peeq.example/?v=b&t=20", Title: "Second"},
	}
	text := "1. No link\n2. First — Chan\n   1. a chapter list\n3. Second — Other"
	out := relabelStructuredSources(text, sources, reg)
	wantHeader := "Source [3] (result 2): First — https://peeq.example/?v=a&t=10\nSource [4] (result 3): Second — https://peeq.example/?v=b&t=20\n\n"
	if out != wantHeader+text {
		t.Fatalf("relabeled =\n%s\nwant the header then the untouched text", out)
	}
	got := reg.all()
	if len(got) != 2 || got[0].Label != "peeq · Chan" || got[0].Snippet != "one" || got[1].Label != "Peeq" {
		t.Fatalf("registered sources = %+v", got)
	}
}

// A numbered list inside a single-source result stays exactly as written.
func TestRelabelStructuredSourcesLeavesBodyListsAlone(t *testing.T) {
	reg := newWebSourceRegistryAfter(0)
	text := "Key points:\n1. Intro\n2. Setup"
	out := relabelStructuredSources(text, []mcp.Source{{URL: "https://peeq.example/video/a", Title: "A video"}}, reg)
	if out != "Source [1]: A video — https://peeq.example/video/a\n\n"+text {
		t.Fatalf("relabeled =\n%s", out)
	}
}

func TestRelabelStructuredSourcesWithoutNumberedItemsAddsHeader(t *testing.T) {
	reg := newWebSourceRegistryAfter(0)
	out := relabelStructuredSources("Title: A video\nSummary...", []mcp.Source{{URL: "https://peeq.example/?v=a", Title: "A video"}}, reg)
	if !strings.HasPrefix(out, "Source [1]: A video — https://peeq.example/?v=a\n\n") {
		t.Fatalf("missing source header:\n%s", out)
	}
}

func TestStructuredSourceImageMustBeHTTPS(t *testing.T) {
	reg := newWebSourceRegistryAfter(0)
	relabelStructuredSources("1. a\n2. b", []mcp.Source{
		{URL: "https://peeq.example/?v=a", Image: "https://i.ytimg.com/vi/a/mqdefault.jpg"},
		{URL: "https://peeq.example/?v=b", Image: "javascript:alert(1)"},
	}, reg)
	got := webSourceCitations(reg.all())
	if got[0].Image != "https://i.ytimg.com/vi/a/mqdefault.jpg" || got[1].Image != "" {
		t.Fatalf("images = %q, %q", got[0].Image, got[1].Image)
	}
}

func TestRelabelStructuredSourcesSkipsUnusableURLs(t *testing.T) {
	reg := newWebSourceRegistryAfter(0)
	out := relabelStructuredSources("1. x", []mcp.Source{{URL: "ftp://nope"}}, reg)
	if out != "1. x" || reg.len() != 0 {
		t.Fatalf("out=%q sources=%d, want untouched", out, reg.len())
	}
}

// A server limited to other accounts is neither offered nor callable, and an
// offered server's instructions reach the system prompt.
func TestTurnGatesRestrictedServerAndCarriesItsGuidance(t *testing.T) {
	peeqTool := llm.Tool{Type: "function", Function: llm.ToolFunction{Name: "peeq__search"}}
	for _, c := range []struct {
		name    string
		users   []string
		offered bool
	}{
		{"allowed", []string{testUser.ID}, true},
		{"denied", []string{"someone_else"}, false},
		// The username is the IdP's mutable claim: it never grants access.
		{"username is no key", []string{testUser.Username}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
			llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
				{ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.ToolCallFunction{Name: "peeq__search", Arguments: `{"query":"rust"}`}}}},
				{Content: "Done."},
			}}
			called := 0
			out := runStoredTurn(t, Config{
				LLM: llmClient,
				MCP: fakeMCPService{
					ToolList:     []llm.Tool{peeqTool},
					Users:        map[string][]string{"peeq": c.users},
					Instructions: map[string]string{"peeq": "Search peeq for videos about rust."},
					Sources:      []mcp.Source{{URL: "https://peeq.example/?v=a&t=5", Title: "Rust talk", Label: "peeq · Chan"}},
					CallFunc: func(context.Context, string, map[string]any) (string, error) {
						called++
						return "1. Rust talk — Chan", nil
					},
				},
			}, store, "what do my channels say about rust?")
			if out.err != nil {
				t.Fatalf("turn error: %v", out.err)
			}
			offered := len(llmClient.Tools) > 0 && hasTool(llmClient.Tools[0], "peeq__search")
			guided := len(llmClient.Histories) > 0 && strings.Contains(llmClient.Histories[0][0].Content, "Search peeq for videos about rust.")
			if offered != c.offered || guided != c.offered {
				t.Fatalf("offered=%v guided=%v, want %v", offered, guided, c.offered)
			}
			if c.offered {
				if called != 1 || !strings.Contains(out.body, "Source [1]: Rust talk") || !strings.Contains(out.body, "peeq · Chan") {
					t.Fatalf("called=%d, body missing relabeled source:\n%s", called, out.body)
				}
				return
			}
			if called != 0 || !strings.Contains(out.body, `unknown tool \"peeq__search\"`) {
				t.Fatalf("denied tool ran (%d calls) or was not refused:\n%s", called, out.body)
			}
		})
	}
}

func hasTool(tools []llm.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}
