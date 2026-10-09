package turn

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/classifier"
	"github.com/trick77/loom/internal/docgen"
	"github.com/trick77/loom/internal/llm"
)

// When the model batches more calls of one tool than its per-round cap allows,
// loom must not abort the turn. It runs up to the cap and defers the rest with a
// tool result, then completes with a normal final answer.
func TestTurnDefersDefaultToolCallsBeyondCap(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	// Round 1 batches one search past the default per-round cap; round 2 concludes.
	round1 := make([]llm.ToolCall, maxToolCallsPerRound+1)
	for i := range round1 {
		round1[i] = llm.ToolCall{ID: "call_search", Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{}`}}
	}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: round1},
		{Content: "Final answer."},
	}}
	calls := map[string]int{}
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
				calls[name]++
				return "search result", nil
			},
		},
	}, store, "Search Lume")

	body := out.body
	if out.err != nil || strings.Contains(body, "too many tool calls") {
		t.Fatalf("turn must not abort on tool-call overflow: err=%v\n%s", out.err, body)
	}
	if calls["search__web"] != maxToolCallsPerRound {
		t.Fatalf("search__web executed %d times, want %d (one deferred)", calls["search__web"], maxToolCallsPerRound)
	}
	if !strings.Contains(body, "Deferred:") {
		t.Fatalf("SSE body missing deferred tool result:\n%s", body)
	}
	if store.AssistantContent != "Final answer." {
		t.Fatalf("assistantContent = %q, want final answer", store.AssistantContent)
	}
}

// The cheap fetch/obscura tools get a higher per-round cap than the default, so a
// paste with a dozen links runs them all in one round.
func TestTurnDefersCheapToolCallsBeyondHigherCap(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	round1 := make([]llm.ToolCall, cheapToolCallsPerRound+1)
	for i := range round1 {
		round1[i] = llm.ToolCall{ID: "call_fetch", Function: llm.ToolCallFunction{Name: fetchToolName, Arguments: `{"url":"https://example.com"}`}}
	}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: round1},
		{Content: "Fetched the links."},
	}}
	calls := map[string]int{}
	var callsMu sync.Mutex // a round's fetches run concurrently
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: fetchToolName}}},
			CallFunc: func(_ context.Context, name string, _ map[string]any) (string, error) {
				callsMu.Lock()
				defer callsMu.Unlock()
				calls[name]++
				return "page contents", nil
			},
		},
	}, store, "Fetch these")

	body := out.body
	if out.err != nil || strings.Contains(body, "too many tool calls") {
		t.Fatalf("turn must not abort on fetch overflow: err=%v\n%s", out.err, body)
	}
	if calls[fetchToolName] != cheapToolCallsPerRound {
		t.Fatalf("fetch executed %d times, want %d (higher cheap-tool cap, one deferred)", calls[fetchToolName], cheapToolCallsPerRound)
	}
	if !strings.Contains(body, "Deferred:") {
		t.Fatalf("SSE body missing deferred tool result:\n%s", body)
	}
	if store.AssistantContent != "Fetched the links." {
		t.Fatalf("assistantContent = %q, want final answer", store.AssistantContent)
	}
}

// A round's web fetches are independent network reads: they run concurrently,
// but their results reach the browser, the history and the [n] source numbering
// in the order the model issued them.
func TestTurnRunsARoundsFetchesConcurrentlyInCallOrder(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	urls := []string{"https://a.example/1", "https://b.example/2", "https://c.example/3"}
	round1 := make([]llm.ToolCall, len(urls))
	for i, u := range urls {
		round1[i] = llm.ToolCall{ID: fmt.Sprintf("call_%d", i+1), Function: llm.ToolCallFunction{Name: fetchToolName, Arguments: fmt.Sprintf(`{"url":%q}`, u)}}
	}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: round1},
		{Content: "Fetched the links."},
	}}
	// Every fetch blocks until all of them are in flight, so a serial loop
	// would only get past the first one by timing out.
	var inFlight sync.WaitGroup
	inFlight.Add(len(urls))
	allStarted := make(chan struct{})
	go func() { inFlight.Wait(); close(allStarted) }()
	var serial atomic.Bool
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: fetchToolName}}},
			CallFunc: func(_ context.Context, _ string, args map[string]any) (string, error) {
				inFlight.Done()
				select {
				case <-allStarted:
				case <-time.After(2 * time.Second):
					serial.Store(true)
				}
				url, _ := args["url"].(string)
				// The first call finishes last: completion order is the reverse of call order.
				time.Sleep(time.Duration(len(urls)-slices.Index(urls, url)) * 5 * time.Millisecond)
				return "page " + url, nil
			},
		},
	}, store, "Fetch these")

	if serial.Load() {
		t.Fatal("fetches ran one after another, want them in flight together")
	}
	body := out.body
	last := -1
	for i, u := range urls {
		at := strings.Index(body, fmt.Sprintf(`Web source [%d]: %s`, i+1, u))
		if at < 0 {
			t.Fatalf("missing tool result numbering %s as source [%d]:\n%s", u, i+1, body)
		}
		if at < last {
			t.Fatalf("tool results out of call order at %s:\n%s", u, body)
		}
		last = at
	}
	if store.AssistantContent != "Fetched the links." {
		t.Fatalf("assistantContent = %q, want final answer", store.AssistantContent)
	}
}

func TestTurnUsesFinalNoToolCallAfterRoundExhaustion(t *testing.T) {
	store := &fakeThreadStore{
		Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	results := make([]llm.StreamResult, maxToolRounds)
	for i := range results {
		results[i] = llm.StreamResult{ToolCalls: []llm.ToolCall{{
			ID: "call_round",
			Function: llm.ToolCallFunction{
				Name:      "search__web",
				Arguments: `{}`,
			},
		}}}
	}
	llmClient := &fakeToolChatClient{Results: results, Plain: "Final answer without more tools."}
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{
			ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			Result:   "search result",
		},
	}, store, "Search Lume")

	// The handler reports "empty assistant response" for exactly this case.
	if out.err == nil && strings.TrimSpace(out.result.Content) == "" {
		t.Fatalf("turn returned an empty response after round exhaustion:\n%s", out.body)
	}
	if store.AssistantContent != "Final answer without more tools." {
		t.Fatalf("assistantContent = %q, want final no-tool answer", store.AssistantContent)
	}
}

// TestTurnGatesToolsByCategory drives a full turn through classification, the
// tool gate, and availableTools, then inspects the exact tool array handed to
// the LLM — the end-to-end proof that a plain turn ships a trimmed set while a
// coding turn gets the coding-relevant tools. It exercises the real path a
// manual run would, but deterministically.
func TestTurnGatesToolsByCategory(t *testing.T) {
	run := func(t *testing.T, category, content string) map[string]bool {
		t.Helper()
		llmClient := &fakeToolChatClient{
			ClassifyResult: category,
			TitleResult:    "T",
			Results:        []llm.StreamResult{{Content: "ok"}},
		}
		out := runStoredTurn(t, Config{
			Artifacts: fakeArtifactStore{},
			UsersDir:  t.TempDir(),
			DocTools:  []docgen.Generator{docgen.TextGenerator{}},
			LLM:       llmClient,
			MCP: fakeMCPService{
				ToolList: []llm.Tool{
					{Type: "function", Function: llm.ToolFunction{Name: "search__web", Description: "Search the web"}},
					{Type: "function", Function: llm.ToolFunction{Name: "context7__query-docs", Description: "Library docs"}},
				},
				ToolCategories: map[string][]string{"context7__query-docs": {string(classifier.Coding)}},
			},
		}, &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle}}, content)
		if out.err != nil {
			t.Fatalf("turn failed: %v body=%s", out.err, out.body)
		}
		if len(llmClient.Tools) != 1 {
			t.Fatalf("tool rounds = %d, want 1; body=%s", len(llmClient.Tools), out.body)
		}
		return toolNames(llmClient.Tools[0])
	}

	t.Run("general turn ships a trimmed set", func(t *testing.T) {
		names := run(t, string(classifier.General), "please tell me a short friendly greeting")
		// Always-on core + category-neutral MCP remain.
		for _, want := range []string{conversationSearchToolName, "search__web"} {
			if !names[want] {
				t.Fatalf("general turn missing always-on tool %q; got %v", want, names)
			}
		}
		// Gated groups are withheld.
		if names["create_text_file"] {
			t.Fatalf("general turn should not offer docgen tools; got %v", names)
		}
		if names["context7__query-docs"] {
			t.Fatalf("general turn should not offer coding-tagged context7; got %v", names)
		}
	})

	t.Run("coding turn gets docgen and context7", func(t *testing.T) {
		names := run(t, string(classifier.Coding), "please help me understand this program")
		if !names["create_text_file"] {
			t.Fatalf("coding turn should offer docgen tools; got %v", names)
		}
		if !names["context7__query-docs"] {
			t.Fatalf("coding turn should offer coding-tagged context7; got %v", names)
		}
	})
}
