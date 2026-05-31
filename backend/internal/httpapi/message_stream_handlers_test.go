package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/spark/internal/chat"
	"github.com/trick77/spark/internal/llm"
	"github.com/trick77/spark/internal/mcp"
)

func TestStreamMessageEmitsDeltasAndPersistsAssistant(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{title: "Greeting"},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"event: user_message",
		"event: assistant_delta",
		`data: {"content":"Hel"}`,
		"event: assistant_message",
		"event: thread",
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	threadEvent := strings.Index(body, "event: thread")
	assistantDelta := strings.Index(body, "event: assistant_delta")
	if threadEvent < 0 || assistantDelta < 0 || threadEvent > assistantDelta {
		t.Fatalf("thread title event index = %d, assistant delta index = %d, want title before assistant response:\n%s", threadEvent, assistantDelta, body)
	}
	if store.assistantContent != "Hello" {
		t.Fatalf("assistantContent = %q, want Hello", store.assistantContent)
	}
	if len(store.messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.messages))
	}
	if store.messages[0].Role != chat.RoleUser || store.messages[0].Content != "Hi" {
		t.Fatalf("first persisted message = %#v, want user Hi", store.messages[0])
	}
	if store.messages[1].Role != chat.RoleAssistant || store.messages[1].Content != "Hello" {
		t.Fatalf("second persisted message = %#v, want assistant Hello", store.messages[1])
	}
}

func TestStreamMessageSendsAndPersistsReasoningContent(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	streamText := "Answer."
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM: fakeChatClient{
			streamText:    &streamText,
			reasoningText: "I should reason first.",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: assistant_reasoning_delta") {
		t.Fatalf("body missing assistant_reasoning_delta:\n%s", body)
	}
	if !strings.Contains(body, `"content":"I should reason first."`) {
		t.Fatalf("body missing reasoning content:\n%s", body)
	}
	if len(store.messages) == 0 {
		t.Fatal("no messages persisted")
	}
	last := store.messages[len(store.messages)-1]
	if last.Role != chat.RoleAssistant || last.ReasoningContent != "I should reason first." {
		t.Fatalf("persisted assistant = %#v", last)
	}
}

func TestStreamMessagePersistsAssistantTokenUsage(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM: fakeChatClient{usage: llm.TokenUsage{
			PromptTokens:     7,
			CompletionTokens: 3,
			TotalTokens:      10,
			PromptTokensDetails: llm.PromptTokenDetails{
				CachedTokens: 5,
			},
			CompletionTokenDetails: llm.CompletionTokenDetails{
				ReasoningTokens: 2,
			},
		}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if len(store.messages) != 2 {
		t.Fatalf("persisted messages = %d, want 2", len(store.messages))
	}
	assistant := store.messages[1]
	if got := derefInt(assistant.PromptTokens); got != 7 {
		t.Fatalf("PromptTokens = %d, want 7", got)
	}
	if got := derefInt(assistant.CompletionTokens); got != 3 {
		t.Fatalf("CompletionTokens = %d, want 3", got)
	}
	if got := derefInt(assistant.TotalTokens); got != 10 {
		t.Fatalf("TotalTokens = %d, want 10", got)
	}
	if got := derefInt(assistant.CachedTokens); got != 5 {
		t.Fatalf("CachedTokens = %d, want 5", got)
	}
	if got := derefInt(assistant.ReasoningTokens); got != 2 {
		t.Fatalf("ReasoningTokens = %d, want 2", got)
	}
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func TestStreamMessagePersistsAssistantAfterClientContextCancel(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	var cancel context.CancelFunc
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM: fakeChatClient{afterStream: func() {
			cancel()
		}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)
	ctx, cancelRequest := context.WithCancel(req.Context())
	cancel = cancelRequest
	req = req.WithContext(ctx)

	srv.ServeHTTP(rec, req)

	if store.assistantContent != "Hello" {
		t.Fatalf("assistantContent = %q, want Hello", store.assistantContent)
	}
	if store.assistantContextErr != nil {
		t.Fatalf("assistant AddMessage context error = %v, want nil", store.assistantContextErr)
	}
}

func TestStreamMessageRejectsEmptyAssistantResponse(t *testing.T) {
	empty := ""
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{streamText: &empty},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"error":"empty assistant response"`) {
		t.Fatalf("SSE body missing empty response error:\n%s", body)
	}
	if len(store.messages) != 1 || store.messages[0].Role != chat.RoleUser {
		t.Fatalf("persisted messages = %#v, want only user message", store.messages)
	}
}

func TestStreamMessageBuildsResponseLanguageHistory(t *testing.T) {
	var history []llm.Message
	store := &fakeChatStore{
		thread:   chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
		messages: []chat.Message{{ID: "old_1", ThreadID: "thr_1", Role: chat.RoleAssistant, Content: "Earlier answer"}},
	}
	user := testUser
	user.ResponseLanguage = "de"
	srv := newAuthenticatedChatServerForUser(t, user, Deps{
		Chat: store,
		LLM:  fakeChatClient{history: &history},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Neue Frage"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(history) != 3 {
		t.Fatalf("history len = %d, want 3: %#v", len(history), history)
	}
	if !strings.Contains(history[0].Content, "Always answer in this language: de.") {
		t.Fatalf("system prompt = %q, want response-language directive", history[0].Content)
	}
	if history[1].Role != "assistant" || history[1].Content != "Earlier answer" {
		t.Fatalf("prior message = %#v", history[1])
	}
	if history[2].Role != "user" || history[2].Content != "Neue Frage" {
		t.Fatalf("new user message = %#v", history[2])
	}
}

func TestStreamMessageReturns503WhenLLMDependencyMissing(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedChatServer(t, Deps{Chat: store})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"error":"llm is not configured"`) {
		t.Fatalf("body = %q, want llm configuration error", rec.Body.String())
	}
}

func TestStreamMessageStillCompletesWhenTitleGenerationFails(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{titleErr: errors.New("title model unavailable")},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: assistant_message") {
		t.Fatalf("SSE body missing assistant message:\n%s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Fatalf("SSE body missing done despite title failure:\n%s", body)
	}
	if strings.Contains(body, "event: error") {
		t.Fatalf("SSE body contains error for best-effort title failure:\n%s", body)
	}
}

func TestStreamMessageEmitsMcpStatus(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{title: "Greeting"},
		MCP: fakeMCPService{statuses: []mcp.ServerStatus{
			{Name: "alpha", Active: true},
			{Name: "zeta", Active: false},
		}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: mcp_status") {
		t.Fatalf("SSE body missing mcp_status event:\n%s", body)
	}
	if !strings.Contains(body, `data: {"active":1,"configured":2}`) {
		t.Fatalf("SSE body missing mcp_status payload:\n%s", body)
	}
}

func TestStreamMessageOmitsMcpStatusWhenNoneConfigured(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  fakeChatClient{title: "Greeting"},
		MCP:  fakeMCPService{},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`)

	srv.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "event: mcp_status") {
		t.Fatalf("SSE body should omit mcp_status when none configured:\n%s", rec.Body.String())
	}
}

func TestStreamMessageExecutesToolCallAndResumesAssistantStream(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{
				ReasoningContent: "I should search first.",
				ToolCalls: []llm.ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: llm.ToolCallFunction{
						Name:      "search__web",
						Arguments: `{"q":"spark"}`,
					},
				}},
			},
			{Content: "I found Spark."},
		},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
		MCP: fakeMCPService{
			tools: []llm.Tool{{
				Type: "function",
				Function: llm.ToolFunction{
					Name:        "search__web",
					Description: "Search the web",
					Parameters:  map[string]any{"type": "object"},
				},
			}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Spark"}`)

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"event: tool_call",
		`"name":"search__web"`,
		"event: tool_result",
		`"content":"search result"`,
		"event: assistant_delta",
		`data: {"content":"I found Spark."}`,
		"event: assistant_message",
		"event: done",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	if store.assistantContent != "I found Spark." {
		t.Fatalf("assistantContent = %q, want final answer", store.assistantContent)
	}
	if len(llmClient.histories) != 2 {
		t.Fatalf("stream calls = %d, want 2", len(llmClient.histories))
	}
	lastHistory := llmClient.histories[1]
	if len(lastHistory) < 3 {
		t.Fatalf("last history too short: %#v", lastHistory)
	}
	if got := lastHistory[len(lastHistory)-1]; got.Role != "tool" || got.ToolCallID != "call_1" || got.Content != "search result" {
		t.Fatalf("last history message = %#v, want tool result", got)
	}
	if got := lastHistory[len(lastHistory)-2]; got.Role != "assistant" || got.ReasoningContent != "I should search first." {
		t.Fatalf("assistant tool-call history = %#v, want preserved reasoning content", got)
	}
}

func TestStreamMessageRecoversFromToolError(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &fakeToolChatClient{
		results: []llm.StreamResult{
			{ToolCalls: []llm.ToolCall{{
				ID: "call_1",
				Function: llm.ToolCallFunction{
					Name:      "search__web",
					Arguments: `{"q":"spark"}`,
				},
			}}},
			{Content: "The search tool failed, but I can continue."},
		},
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
		MCP: fakeMCPService{
			tools: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			err:   errFakeTool,
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Spark"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "event: error") {
		t.Fatalf("SSE body contains turn-level error for tool failure:\n%s", body)
	}
	if !strings.Contains(body, "event: tool_result") || !strings.Contains(body, "tool failed: fake tool failed") {
		t.Fatalf("SSE body missing tool failure result:\n%s", body)
	}
	if store.assistantContent != "The search tool failed, but I can continue." {
		t.Fatalf("assistantContent = %q, want recovered answer", store.assistantContent)
	}
}

func TestStreamMessageStopsAfterToolCallLimit(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	toolCalls := make([]llm.ToolCall, maxToolCallsPerRound+1)
	for i := range toolCalls {
		toolCalls[i] = llm.ToolCall{
			ID: "call_limit",
			Function: llm.ToolCallFunction{
				Name:      "search__web",
				Arguments: `{}`,
			},
		}
	}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  &fakeToolChatClient{results: []llm.StreamResult{{ToolCalls: toolCalls}}},
		MCP:  fakeMCPService{tools: []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}}},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Spark"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "too many tool calls") {
		t.Fatalf("SSE body missing tool-call-limit error:\n%s", body)
	}
	if len(store.messages) != 1 || store.messages[0].Role != chat.RoleUser {
		t.Fatalf("persisted messages = %#v, want only user message", store.messages)
	}
}

func TestStreamMessageUsesFinalNoToolCallAfterRoundExhaustion(t *testing.T) {
	store := &fakeChatStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
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
	llmClient := &fakeToolChatClient{results: results, plain: "Final answer without more tools."}
	srv := newAuthenticatedChatServer(t, Deps{
		Chat: store,
		LLM:  llmClient,
		MCP: fakeMCPService{
			tools:  []llm.Tool{{Type: "function", Function: llm.ToolFunction{Name: "search__web"}}},
			result: "search result",
		},
	})
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Search Spark"}`)

	srv.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "empty assistant response") {
		t.Fatalf("SSE body returned empty response after round exhaustion:\n%s", body)
	}
	if store.assistantContent != "Final answer without more tools." {
		t.Fatalf("assistantContent = %q, want final no-tool answer", store.assistantContent)
	}
}
