package turn

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// fakeInbox hands out its messages once, after `after` HasQueued calls said
// there was nothing yet.
type fakeInbox struct {
	store    *fakeThreadStore
	contents []string
	consumed bool
	checks   int
}

func (f *fakeInbox) HasQueued(context.Context) (bool, error) {
	f.checks++
	return !f.consumed && len(f.contents) > 0, nil
}

func (f *fakeInbox) Consume(ctx context.Context) ([]chat.Message, error) {
	if f.consumed {
		return nil, nil
	}
	f.consumed = true
	var out []chat.Message
	for _, c := range f.contents {
		m, err := f.store.AddMessageWithAttachments(ctx, testUser.ID, f.store.Thread.ID, chat.RoleUser, c, nil, nil, "send-q")
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func searchTool() fakeMCPService {
	return fakeMCPService{
		ToolList: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
		CallFunc: func(context.Context, string, map[string]any) (string, error) {
			return "Lume is a lamp from 2019.", nil
		},
	}
}

func TestQueuedMessageIsPickedUpAfterAToolRound(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ReasoningContent: "look it up", ToolCalls: []llm.ToolCall{{ID: "call_1", Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{}`}}}},
		{ReasoningContent: "answer both", Content: "Both answered."},
	}}
	inbox := &fakeInbox{store: store, contents: []string{"And who made it?"}}
	out := runStoredTurnWithInbox(t, Config{LLM: llmClient, MCP: searchTool()}, store, "What is Lume?", inbox)
	if out.err != nil {
		t.Fatalf("turn error = %v", out.err)
	}

	var order []string
	for _, m := range store.Messages {
		order = append(order, string(m.Role)+":"+m.Content)
	}
	want := []string{"user:What is Lume?", "assistant:", "user:And who made it?", "assistant:Both answered."}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("transcript = %q, want %q", order, want)
	}
	segment := store.Messages[1]
	if !strings.Contains(string(segment.ContentBlocks), "call_1") {
		t.Fatalf("segment blocks = %s, want the tool round", segment.ContentBlocks)
	}
	if strings.Contains(string(store.LastContentBlocks), "call_1") {
		t.Fatalf("final blocks = %s, must not repeat the segment's tool round", store.LastContentBlocks)
	}

	// The second call answers from a DB-shaped history: the first question, the
	// segment carrying what the search found, then the queued question.
	second := llmClient.Histories[1]
	last := second[len(second)-1]
	if last.Role != "user" || last.Content != "And who made it?" {
		t.Fatalf("last history message = %+v, want the queued question", last)
	}
	prev := second[len(second)-2]
	if prev.Role != "assistant" || !strings.Contains(prev.Content, "Lume is a lamp from 2019.") || len(prev.ToolCalls) != 0 {
		t.Fatalf("segment history message = %+v, want tool notes without tool calls", prev)
	}
	for _, m := range second {
		if m.Role == "tool" {
			t.Fatalf("history still carries a raw tool row: %+v", m)
		}
	}

	body := out.body
	segIdx := strings.Index(body, "event: assistant_segment")
	userIdx := strings.LastIndex(body, "event: user_message")
	if segIdx == -1 || userIdx == -1 || userIdx < segIdx {
		t.Fatalf("want assistant_segment then user_message in the stream:\n%s", body)
	}

	// Reasoning ids continue across the split, so a title cannot land on the
	// other segment's reasoning.
	var finalTrace []ActivityTraceEvent
	if err := json.Unmarshal(json.RawMessage(store.Messages[3].ActivityTrace), &finalTrace); err != nil {
		t.Fatal(err)
	}
	if len(finalTrace) == 0 || finalTrace[0].ID != "reasoning-2" {
		t.Fatalf("final trace = %+v, want it to start at reasoning-2", finalTrace)
	}
}

func TestNoQueuedMessageLeavesTheTurnWhole(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{
		{ToolCalls: []llm.ToolCall{{ID: "call_1", Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{}`}}}},
		{Content: "Answer."},
	}}
	inbox := &fakeInbox{store: store}
	out := runStoredTurnWithInbox(t, Config{LLM: llmClient, MCP: searchTool()}, store, "What is Lume?", inbox)
	if out.err != nil {
		t.Fatal(out.err)
	}
	if len(store.Messages) != 2 || inbox.checks == 0 {
		t.Fatalf("messages = %d, inbox checks = %d; want 2 messages and the inbox asked", len(store.Messages), inbox.checks)
	}
	if strings.Contains(out.body, "assistant_segment") {
		t.Fatal("no split expected without a queued message")
	}
}

// endlessInbox always has another message queued.
type endlessInbox struct {
	store    *fakeThreadStore
	consumed int
}

func (*endlessInbox) HasQueued(context.Context) (bool, error) { return true, nil }

func (f *endlessInbox) Consume(ctx context.Context) ([]chat.Message, error) {
	f.consumed++
	m, err := f.store.AddMessageWithAttachments(ctx, testUser.ID, f.store.Thread.ID, chat.RoleUser, "more", nil, nil, "send-q")
	return []chat.Message{m}, err
}

// Queueing on every tool round cannot keep one turn alive without end: past
// maxQueuedPickups the turn keeps its budget and the rest waits.
func TestQueuedPickupsPerTurnAreCapped(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	results := make([]llm.StreamResult, 0, 40)
	for range 39 {
		results = append(results, llm.StreamResult{ToolCalls: []llm.ToolCall{{ID: "call", Function: llm.ToolCallFunction{Name: "search__web", Arguments: `{}`}}}})
	}
	llmClient := &fakeToolChatClient{Results: append(results, llm.StreamResult{Content: "done"}), Plain: "final"}
	inbox := &endlessInbox{store: store}
	out := runStoredTurnWithInbox(t, Config{LLM: llmClient, MCP: searchTool()}, store, "Go", inbox)
	if out.err != nil {
		t.Fatal(out.err)
	}
	if inbox.consumed != maxQueuedPickups {
		t.Fatalf("pickups = %d, want %d", inbox.consumed, maxQueuedPickups)
	}
	if calls := len(llmClient.Histories); calls > (maxQueuedPickups+1)*maxToolRounds+1 {
		t.Fatalf("model calls = %d, the round budget was not kept", calls)
	}
}

// A model that answers without tools never reaches a boundary: the queued
// message waits for a follow-up turn.
func TestQueuedMessageWaitsWhenTheModelAnswersWithoutTools(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &fakeToolChatClient{Results: []llm.StreamResult{{Content: "Direct answer."}}}
	inbox := &fakeInbox{store: store, contents: []string{"later"}}
	out := runStoredTurnWithInbox(t, Config{LLM: llmClient, MCP: searchTool()}, store, "Hi", inbox)
	if out.err != nil {
		t.Fatal(out.err)
	}
	if inbox.consumed {
		t.Fatal("queued message consumed without a tool boundary")
	}
}
