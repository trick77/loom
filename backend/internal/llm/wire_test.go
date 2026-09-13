package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/llmwire"
)

func int64p(v int64) *int64 { return &v }

func TestToWireMessages_CarriesPartsToolCallsAndReasoning(t *testing.T) {
	got := toWireMessages([]Message{
		{Role: "system", Content: "be brief"},
		{Role: "user", ContentParts: []MessageContentPart{
			{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,AAAA"}},
			{Type: "text", Text: "what is this"},
		}},
		{Role: "assistant", ReasoningContent: "thought", ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: ToolCallFunction{Name: "search", Arguments: `{"q":"x"}`}}}},
		{Role: "tool", ToolCallID: "c1", Content: "result"},
	})
	if len(got) != 4 {
		t.Fatalf("got %d messages", len(got))
	}
	if got[0].Role != llmwire.RoleSystem || got[0].Text != "be brief" {
		t.Errorf("system = %#v", got[0])
	}
	if len(got[1].Parts) != 2 || got[1].Parts[0].Kind != llmwire.PartImage || got[1].Parts[0].URL != "data:image/png;base64,AAAA" || got[1].Parts[1].Kind != llmwire.PartText || got[1].Parts[1].Text != "what is this" {
		t.Errorf("parts = %#v", got[1].Parts)
	}
	if got[2].ReasoningContent != "thought" || len(got[2].ToolCalls) != 1 || got[2].ToolCalls[0].Name != "search" || got[2].ToolCalls[0].Arguments != `{"q":"x"}` {
		t.Errorf("assistant = %#v", got[2])
	}
	if got[3].Role != llmwire.RoleTool || got[3].ToolCallID != "c1" || got[3].Text != "result" {
		t.Errorf("tool = %#v", got[3])
	}
}

func TestToWireTools_FlattensTheFunctionEnvelope(t *testing.T) {
	if toWireTools(nil) != nil {
		t.Fatal("no tools must stay nil so nothing is rendered")
	}
	got := toWireTools([]Tool{{Type: "function", Function: ToolFunction{Name: "search", Description: "d", Parameters: map[string]any{"type": "object"}}}})
	if len(got) != 1 || got[0].Name != "search" || got[0].Description != "d" || got[0].Parameters["type"] != "object" {
		t.Fatalf("tools = %#v", got)
	}
}

func TestFromWireToolCall_DefaultsTheType(t *testing.T) {
	got := fromWireToolCall(llmwire.ToolCall{ID: "inline_call_1", Name: "a", Arguments: "{}"})
	if got.Type != "function" || got.ID != "inline_call_1" || got.Function.Name != "a" || got.Function.Arguments != "{}" {
		t.Fatalf("call = %#v", got)
	}
}

func TestUsageFromWire_FlattensLanesAndKeepsUnreportedEmpty(t *testing.T) {
	var u llmwire.Usage
	if got := usageFromWire(u); got.Present() {
		t.Fatalf("unreported usage = %#v, want empty", got)
	}
	u.Input.Total, u.Input.CacheRead = int64p(100), int64p(40)
	u.Output.Total, u.Output.Reasoning = int64p(30), int64p(12)
	got := usageFromWire(u)
	if got.PromptTokens != 100 || got.CompletionTokens != 30 || got.TotalTokens != 130 || got.PromptTokensDetails.CachedTokens != 40 || got.CompletionTokenDetails.ReasoningTokens != 12 {
		t.Fatalf("usage = %#v", got)
	}
}

func TestCostFromWire_UnpricedIsUnknownNotZero(t *testing.T) {
	var u llmwire.Usage
	if nano, priced := costFromWire(u); priced || nano != 0 {
		t.Fatalf("unpriced = %d/%v", nano, priced)
	}
	u.Cost = llmwire.Cost{NanoUSD: 4200, Provenance: llmwire.FromTable}
	if nano, priced := costFromWire(u); !priced || nano != 4200 {
		t.Fatalf("priced = %d/%v", nano, priced)
	}
}

func TestChatError_PhrasesStatusAndStall(t *testing.T) {
	if chatError(nil) != nil {
		t.Fatal("nil in, nil out")
	}
	status := chatError(&llmwire.APIError{StatusCode: 502, Message: "bad gateway"})
	if status.Error() != "chat completion failed with status 502: bad gateway" {
		t.Fatalf("status error = %q", status)
	}
	idle := chatError(errors.New("llmwire: " + llmwire.ErrStreamIdle.Error() + " for 1m"))
	if errors.Is(idle, ErrStreamStalled) {
		t.Fatal("a string match must not count as the sentinel")
	}
	if err := chatError(errorWrapping(llmwire.ErrStreamIdle)); !errors.Is(err, ErrStreamStalled) {
		t.Fatalf("idle = %v, want ErrStreamStalled", err)
	}
	other := chatError(errors.New("boom"))
	if !strings.HasPrefix(other.Error(), "chat completion request: boom") {
		t.Fatalf("other = %q", other)
	}
}

type wrapped struct{ inner error }

func (w wrapped) Error() string { return "wrapped: " + w.inner.Error() }
func (w wrapped) Unwrap() error { return w.inner }

func errorWrapping(err error) error { return wrapped{inner: err} }

// A streamed turn's cost lands on the result and in the accumulator on the
// context; the helper calls record theirs too, so the turn total covers every
// call the way the token counts do.
func TestStreamChat_RecordsPricedCostIntoTheAccumulator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":100,\"prompt_tokens_details\":{\"cached_tokens\":0},\"completion_tokens_details\":{\"reasoning_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := mustClient(t, Config{BaseURL: server.URL, Timeout: 5 * time.Second}, server.Client())
	acc := NewUsageAccumulator()
	ctx := WithUsageAccumulator(context.Background(), acc)
	result, err := client.StreamChatResult(ctx, []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// mimo-v2.5-pro: 1000 input at 0.435 USD/M, 100 output at 0.87 USD/M.
	const want = int64(1000*435 + 100*870)
	if !result.CostPriced || result.CostNanoUSD != want {
		t.Fatalf("cost = %d priced=%v, want %d", result.CostNanoUSD, result.CostPriced, want)
	}
	if nano, priced := acc.Cost(); !priced || nano != want {
		t.Fatalf("accumulated = %d/%v, want %d", nano, priced, want)
	}
	if usage := acc.Total(); usage.PromptTokens != 1000 || usage.CompletionTokens != 100 {
		t.Fatalf("accumulated usage = %#v", usage)
	}
}

func TestUsageAccumulator_UnpricedCallsLeaveTheTotalUnknown(t *testing.T) {
	acc := NewUsageAccumulator()
	acc.addCost(500, false)
	if nano, priced := acc.Cost(); priced || nano != 0 {
		t.Fatalf("after an unpriced call: %d/%v", nano, priced)
	}
	acc.addCost(500, true)
	acc.addCost(250, true)
	if nano, priced := acc.Cost(); !priced || nano != 750 {
		t.Fatalf("after priced calls: %d/%v", nano, priced)
	}
	var nilAcc *UsageAccumulator
	nilAcc.addCost(1, true)
	if nano, priced := nilAcc.Cost(); priced || nano != 0 {
		t.Fatal("a nil accumulator reports unknown")
	}
	RecordCost(context.Background(), 1, true) // no accumulator on ctx: a no-op
}

// The response spool is llmwire's transport now; loom still wires it from
// ResponseLogDir and still keeps incognito turns off disk.
func TestNewClient_SpoolsResponsesExceptIncognito(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "Blue Sky"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 5, "completion_tokens": 2},
		})
	}))
	t.Cleanup(server.Close)
	dir := t.TempDir() + "/spool"
	client := mustClient(t, Config{BaseURL: server.URL, ResponseLogDir: dir}, server.Client())

	incognito := WithInferenceMetadata(context.Background(), InferenceMetadata{Incognito: true})
	if _, err := client.GenerateThreadTitle(incognito, "why is the sky blue", "", ""); err != nil {
		t.Fatal(err)
	}
	if entries, _ := readDirNames(dir); len(entries) != 0 {
		t.Fatalf("incognito turn was spooled: %v", entries)
	}
	if _, err := client.GenerateThreadTitle(context.Background(), "why is the sky blue", "", ""); err != nil {
		t.Fatal(err)
	}
	if entries, _ := readDirNames(dir); len(entries) != 1 || !strings.HasSuffix(entries[0], ".http") {
		t.Fatalf("spool = %v, want one .http file", entries)
	}
}
