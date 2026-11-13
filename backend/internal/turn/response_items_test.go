package turn

import (
	"encoding/json"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

var signedThinking = json.RawMessage(`{"type":"thinking","thinking":"","signature":"sig"}`)

// A tool round appends the model's turn as it came back, signed thinking
// included, so the next round replays it unchanged. The forced final answer
// rebuilds the history, where a replayed block would be refused, so none of
// its messages carries any.
func TestToolLoopReplaysResponseItemsButNotIntoTheFinalAnswer(t *testing.T) {
	call := llm.ToolCall{ID: "c1", Type: "function", Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{"query":"x"}`}}
	llmClient := &fakeToolChatClient{TitleResult: "T", Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{call}, FinishReason: "tool_calls", ResponseItems: []json.RawMessage{signedThinking}},
		// Empty after tools: the loop forces a tool-free final answer.
		{Content: ""},
		{Content: "final"},
	}}
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}}},
	}, &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle}}, "look it up")
	if out.err != nil || len(llmClient.Histories) < 3 {
		t.Fatalf("turn: %v, %d rounds; body=%s", out.err, len(llmClient.Histories), out.body)
	}
	replayed := false
	for _, m := range llmClient.Histories[1] {
		if m.Role == "assistant" && len(m.ResponseItems) == 1 && string(m.ResponseItems[0]) == string(signedThinking) {
			replayed = true
		}
	}
	if !replayed {
		t.Fatalf("round 2 did not replay the signed turn: %+v", llmClient.Histories[1])
	}
	for i, m := range llmClient.Histories[len(llmClient.Histories)-1] {
		if len(m.ResponseItems) > 0 {
			t.Fatalf("final answer message %d replays response items after a rebuilt history", i)
		}
	}
}

// A thread on the deep slot asks for the deep model on its turn's own rounds;
// one on the standard slot does not.
func TestTurnCarriesTheThreadsChatSlot(t *testing.T) {
	for _, tc := range []struct {
		slot string
		want bool
	}{{chat.ChatSlotStandard, false}, {llm.SlotDeep, true}} {
		llmClient := &fakeToolChatClient{TitleResult: "T", Results: []llm.StreamResult{{Content: "ok"}}}
		out := runStoredTurn(t, Config{LLM: llmClient},
			&fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle, ChatSlot: tc.slot}}, "hi")
		if out.err != nil || len(llmClient.Inference) == 0 {
			t.Fatalf("%s: %v, %d calls", tc.slot, out.err, len(llmClient.Inference))
		}
		if got := llmClient.Inference[0].Deep; got != tc.want {
			t.Fatalf("%s: Deep = %v, want %v", tc.slot, got, tc.want)
		}
	}
}

// A model that declines (finish_reason content_filter, nothing written) ends
// the turn with a message saying so, not a generic failure.
func TestDeclinedTurnSaysSo(t *testing.T) {
	llmClient := &fakeToolChatClient{TitleResult: "T", Results: []llm.StreamResult{{FinishReason: "content_filter"}}}
	out := runStoredTurn(t, Config{
		LLM: llmClient,
		MCP: fakeMCPService{ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}}},
	}, &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle}}, "hi")
	if out.err != nil || out.result.Content != declinedMessage {
		t.Fatalf("err %v; answer %q, want the declined message", out.err, out.result.Content)
	}
	// A tool-less turn says so too.
	plain := &fakeToolChatClient{TitleResult: "T", Results: []llm.StreamResult{{FinishReason: "content_filter"}}}
	out = runStoredTurn(t, Config{LLM: plain},
		&fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle}}, "hi")
	if out.err != nil || out.result.Content != declinedMessage {
		t.Fatalf("tool-less decline: err %v, answer %q", out.err, out.result.Content)
	}
}
