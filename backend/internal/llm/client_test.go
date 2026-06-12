package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestClient_StreamChatSendsOpenAICompatibleRequest(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody struct {
		Model               string    `json:"model"`
		Messages            []Message `json:"messages"`
		Stream              bool      `json:"stream"`
		ReasoningEffort     string    `json:"reasoning_effort"`
		MaxCompletionTokens int       `json:"max_completion_tokens"`
		StreamOptions       struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{
		BaseURL: server.URL + "/v1/",
		APIKey:  "secret",
		Model:   "mimo",
	}, server.Client())

	var chunks []string
	final, err := client.StreamChat(context.Background(), []Message{
		{Role: "user", Content: "Hi"},
	}, func(delta string) error {
		chunks = append(chunks, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat() error: %v", err)
	}

	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", gotAuth)
	}
	if gotBody.Model != "mimo" {
		t.Fatalf("model = %q, want mimo", gotBody.Model)
	}
	if !gotBody.Stream {
		t.Fatal("stream = false, want true")
	}
	if !gotBody.StreamOptions.IncludeUsage {
		t.Fatal("stream_options.include_usage = false, want true")
	}
	if gotBody.ReasoningEffort != "high" {
		t.Fatalf("reasoning_effort = %q, want high", gotBody.ReasoningEffort)
	}
	if gotBody.MaxCompletionTokens != 2048 {
		t.Fatalf("max_completion_tokens = %d, want 2048", gotBody.MaxCompletionTokens)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Role != "user" || gotBody.Messages[0].Content != "Hi" {
		t.Fatalf("messages = %#v, want user message", gotBody.Messages)
	}
	if strings.Join(chunks, "") != "Hello" {
		t.Fatalf("chunks = %#v, want Hello", chunks)
	}
	if final != "Hello" {
		t.Fatalf("final = %q, want Hello", final)
	}
}

func TestClient_StreamChatUsesConfiguredMaxCompletionTokens(t *testing.T) {
	var gotBody struct {
		MaxCompletionTokens int `json:"max_completion_tokens"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo", MaxCompletionTokens: 4096}, server.Client())
	result, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("StreamChatResult() error: %v", err)
	}
	if gotBody.MaxCompletionTokens != 4096 {
		t.Fatalf("max_completion_tokens = %d, want 4096", gotBody.MaxCompletionTokens)
	}
	if result.FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", result.FinishReason)
	}
}

func TestClient_StreamChatTimeoutCancelsPrimaryStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"still thinking\"}}]}\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo", Timeout: 10 * time.Millisecond}, server.Client())
	_, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StreamChatResult() error = %v, want context deadline exceeded", err)
	}
}

func TestClient_StreamChatResultCapturesUsageTrailerChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"}}]}\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[],"usage":{"prompt_tokens":7,"completion_tokens":11,"total_tokens":18,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":5}}}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	result, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("StreamChatResult() error: %v", err)
	}
	if result.Content != "Done" {
		t.Fatalf("content = %q, want Done", result.Content)
	}
	wantUsage := TokenUsage{
		PromptTokens:     7,
		CompletionTokens: 11,
		TotalTokens:      18,
		PromptTokensDetails: PromptTokenDetails{
			CachedTokens: 3,
		},
		CompletionTokenDetails: CompletionTokenDetails{
			ReasoningTokens: 5,
		},
	}
	if result.Usage != wantUsage {
		t.Fatalf("usage = %#v, want %#v", result.Usage, wantUsage)
	}
}

func TestClient_StreamChatResultCapturesModelAndReasoningEffortOnDonePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo", ReasoningEffort: "low"}, server.Client())

	result, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("StreamChatResult() error: %v", err)
	}
	if result.Model != "mimo" {
		t.Fatalf("model = %q, want mimo", result.Model)
	}
	if result.ReasoningEffort != "low" {
		t.Fatalf("reasoning effort = %q, want low", result.ReasoningEffort)
	}
}

func TestClient_StreamChatResultCapturesReasoningContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"reasoning_content":"I should check "}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"reasoning_content":"the facts."}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"Answer."}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	var events []StreamEvent
	result, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if result.Content != "Answer." {
		t.Fatalf("content = %q, want Answer.", result.Content)
	}
	if result.ReasoningContent != "I should check the facts." {
		t.Fatalf("reasoning content = %q", result.ReasoningContent)
	}
	if len(events) != 3 {
		t.Fatalf("events = %#v, want 3", events)
	}
	if events[0].ReasoningDelta != "I should check " || events[1].ReasoningDelta != "the facts." || events[2].Delta != "Answer." {
		t.Fatalf("events = %#v, want reasoning deltas then content delta", events)
	}
}

func TestClient_StreamChatResultLeavesUsageEmptyWhenMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	result, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("StreamChatResult() error: %v", err)
	}
	if result.Content != "Done" {
		t.Fatalf("content = %q, want Done", result.Content)
	}
	if result.Usage != (TokenUsage{}) {
		t.Fatalf("usage = %#v, want empty", result.Usage)
	}
	if result.Usage.Present() {
		t.Fatal("usage.Present() = true, want false")
	}
}

func TestClient_StreamChatLogsRawResponseWhenConfigured(t *testing.T) {
	logDir := t.TempDir()
	var responseCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseCount++
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Debug-Trace", "trace-123")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"<!doctype html> response ` + string(rune('0'+responseCount)) + `"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo", ResponseLogDir: logDir}, server.Client())

	for range 2 {
		if _, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil); err != nil {
			t.Fatalf("StreamChat() error: %v", err)
		}
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatalf("ReadDir() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("log entries = %d, want 2", len(entries))
	}
	namePattern := regexp.MustCompile(`^\d{8}T\d{6}\.\d{9}Z-\d{6}\.http$`)
	for index, entry := range entries {
		if !namePattern.MatchString(entry.Name()) {
			t.Fatalf("log file name = %q, want timestamped .http file", entry.Name())
		}
		logBytes, err := os.ReadFile(logDir + "/" + entry.Name())
		if err != nil {
			t.Fatalf("ReadFile() error: %v", err)
		}
		logText := string(logBytes)
		for _, want := range []string{
			"HTTP/1.1 200 OK",
			"Content-Type: text/event-stream",
			"X-Debug-Trace: trace-123",
			`data: {"choices":[{"delta":{"content":"<!doctype html> response ` + string(rune('1'+index)) + `"}}]}`,
			"data: [DONE]",
		} {
			if !strings.Contains(logText, want) {
				t.Fatalf("log text missing %q:\n%s", want, logText)
			}
		}
	}
}

func TestClient_StreamChatOmitsReasoningEffortForNonMiMoModel(t *testing.T) {
	var gotBody struct {
		ReasoningEffort string `json:"reasoning_effort"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "other-model"}, server.Client())

	if _, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil); err != nil {
		t.Fatalf("StreamChat() error: %v", err)
	}
	if gotBody.ReasoningEffort != "" {
		t.Fatalf("reasoning_effort = %q, want omitted", gotBody.ReasoningEffort)
	}
}

func TestClient_StreamChatUsesConfiguredReasoningEffort(t *testing.T) {
	var gotBody struct {
		ReasoningEffort string `json:"reasoning_effort"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{
		BaseURL:         server.URL,
		Model:           "mimo",
		ReasoningEffort: "low",
	}, server.Client())

	if _, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil); err != nil {
		t.Fatalf("StreamChat() error: %v", err)
	}
	if gotBody.ReasoningEffort != "low" {
		t.Fatalf("reasoning_effort = %q, want low", gotBody.ReasoningEffort)
	}
}

func TestClient_StreamChatWithToolsSendsToolSchemas(t *testing.T) {
	var gotBody struct {
		Model string `json:"model"`
		Tools []Tool `json:"tools"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	final, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Hi"}}, []Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "search__web",
			Description: "Search the web",
			Parameters:  map[string]any{"type": "object"},
		},
	}}, nil)
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if final.Content != "Done" {
		t.Fatalf("final content = %q, want Done", final.Content)
	}
	if len(gotBody.Tools) != 1 || gotBody.Tools[0].Function.Name != "search__web" {
		t.Fatalf("tools = %#v", gotBody.Tools)
	}
}

func TestClient_StreamChatWithDocumentToolUsesExpandedCompletionBudget(t *testing.T) {
	var gotBody struct {
		MaxCompletionTokens int `json:"max_completion_tokens"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	_, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Make a PDF"}}, []Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "create_pdf_file",
			Description: "Create a PDF",
			Parameters:  map[string]any{"type": "object"},
		},
	}}, nil)
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if gotBody.MaxCompletionTokens != 8192 {
		t.Fatalf("max_completion_tokens = %d, want 8192", gotBody.MaxCompletionTokens)
	}
}

func TestClient_StreamChatWithNonDocumentToolKeepsConfiguredCompletionBudget(t *testing.T) {
	tests := []struct {
		name                 string
		configuredTokens     int
		toolName             string
		wantCompletionTokens int
	}{
		{
			name:                 "non-document tool uses default budget",
			toolName:             "search__web",
			wantCompletionTokens: defaultMaxCompletionTokens,
		},
		{
			name:                 "document tool preserves higher configured budget",
			configuredTokens:     16384,
			toolName:             "create_pdf_file",
			wantCompletionTokens: 16384,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotBody struct {
				MaxCompletionTokens int `json:"max_completion_tokens"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
					t.Fatalf("Decode request body: %v", err)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"},\"finish_reason\":\"stop\"}]}\n\n"))
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			t.Cleanup(server.Close)

			client := NewClient(Config{BaseURL: server.URL, Model: "mimo", MaxCompletionTokens: tt.configuredTokens}, server.Client())

			_, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Use a tool"}}, []Tool{{
				Type: "function",
				Function: ToolFunction{
					Name:        tt.toolName,
					Description: "Tool",
					Parameters:  map[string]any{"type": "object"},
				},
			}}, nil)
			if err != nil {
				t.Fatalf("StreamChatWithTools() error: %v", err)
			}
			if gotBody.MaxCompletionTokens != tt.wantCompletionTokens {
				t.Fatalf("max_completion_tokens = %d, want %d", gotBody.MaxCompletionTokens, tt.wantCompletionTokens)
			}
		})
	}
}

func TestClient_StreamChatWithToolsReconstructsToolCallDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search__web","arguments":"{\"q\""}}]}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"slopr\"}"}}]}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	var events []StreamEvent
	final, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Search"}}, nil, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if final.Content != "" {
		t.Fatalf("final content = %q, want empty", final.Content)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want 1", final.ToolCalls)
	}
	call := final.ToolCalls[0]
	if call.ID != "call_1" || call.Function.Name != "search__web" || call.Function.Arguments != `{"q":"slopr"}` {
		t.Fatalf("tool call = %#v", call)
	}
	// A pending signal fires the moment the first tool-call chunk arrives, ahead
	// of the fully-reconstructed call emitted at the end of the stream.
	if len(events) != 2 || !events[0].ToolPending {
		t.Fatalf("events = %#v, want a tool-pending event first", events)
	}
	if events[1].ToolPending || events[1].ToolCall.Function.Name != "search__web" {
		t.Fatalf("events = %#v, want final tool call event", events)
	}
}

func TestClient_StreamChatWithToolsParsesMiMoInlineToolCalls(t *testing.T) {
	xml := "<tool_call><function=tavily__tavily_search><parameter=q>colossus</parameter></function></tool_call>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + xml + `"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	var events []StreamEvent
	offeredTools := []Tool{{Type: "function", Function: ToolFunction{Name: "tavily__tavily_search"}}}
	final, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Search"}}, offeredTools, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want 1", final.ToolCalls)
	}
	call := final.ToolCalls[0]
	if call.Function.Name != "tavily__tavily_search" || call.Function.Arguments != `{"q":"colossus"}` {
		t.Fatalf("tool call = %#v", call)
	}
	if final.Content != "" {
		t.Fatalf("final content = %q, want empty (XML stripped)", final.Content)
	}
	sawToolCall := false
	for _, e := range events {
		if e.ToolCall.Function.Name == "tavily__tavily_search" {
			sawToolCall = true
		}
	}
	if !sawToolCall {
		t.Fatalf("events = %#v, want a tool_call event", events)
	}
}

func TestClient_StreamChatWithToolsSignalsPendingBeforeInlineToolCall(t *testing.T) {
	xml := "<tool_call><function=tavily__tavily_search><parameter=q>colossus</parameter></function></tool_call>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"Let me search. "}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + xml + `"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	var events []StreamEvent
	offeredTools := []Tool{{Type: "function", Function: ToolFunction{Name: "tavily__tavily_search"}}}
	_, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Search"}}, offeredTools, func(event StreamEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	// The preamble streams as content, then a pending signal fires the instant the
	// inline marker is seen — ahead of the parsed tool call at end of stream.
	deltaIdx, pendingIdx, callIdx := -1, -1, -1
	for i, e := range events {
		switch {
		case e.ToolPending:
			pendingIdx = i
		case e.ToolCall.Function.Name == "tavily__tavily_search":
			callIdx = i
		case e.Delta != "":
			deltaIdx = i
		}
	}
	if deltaIdx == -1 || pendingIdx == -1 || callIdx == -1 {
		t.Fatalf("events = %#v, want a content, a pending and a tool-call event", events)
	}
	if !(deltaIdx < pendingIdx && pendingIdx < callIdx) {
		t.Fatalf("events out of order: delta=%d pending=%d call=%d (%#v)", deltaIdx, pendingIdx, callIdx, events)
	}
}

func TestClient_StreamChatWithoutToolsKeepsInlineXMLAsContent(t *testing.T) {
	// The tool-free final-answer call must not parse/strip inline tool XML;
	// otherwise a stray inline call would empty the content and discard the turn.
	xml := "<tool_call><function=tavily__tavily_search><parameter=q>colossus</parameter></function></tool_call>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + xml + `"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	final, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Answer"}}, nil, nil)
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if len(final.ToolCalls) != 0 {
		t.Fatalf("tool calls = %#v, want none parsed when no tools offered", final.ToolCalls)
	}
	if final.Content != xml {
		t.Fatalf("final content = %q, want inline XML kept verbatim", final.Content)
	}
}

func TestClient_StreamChatWithToolsDoesNotStreamMiMoInlineXML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Inline tool call arriving as several content chunks, marker split across two.
		for _, c := range []string{"<tool", "_call><function=tavily__tavily_search>", "<parameter=q>x</parameter></function></tool_call>"} {
			_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + c + `"}}]}` + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	var deltas string
	offeredTools := []Tool{{Type: "function", Function: ToolFunction{Name: "tavily__tavily_search"}}}
	final, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Search"}}, offeredTools, func(event StreamEvent) error {
		deltas += event.Delta
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if deltas != "" {
		t.Fatalf("streamed deltas = %q, want none (XML suppressed)", deltas)
	}
	if len(final.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want 1", final.ToolCalls)
	}
}

func TestClient_StreamChatWithToolsStreamsNormalMiMoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, c := range []string{"Colossus ", "is a 1970 ", "film."} {
			_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + c + `"}}]}` + "\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	var deltas string
	final, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, func(event StreamEvent) error {
		deltas += event.Delta
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if deltas != "Colossus is a 1970 film." {
		t.Fatalf("streamed deltas = %q, want full content", deltas)
	}
	if final.Content != "Colossus is a 1970 film." {
		t.Fatalf("final content = %q", final.Content)
	}
}

func TestClient_StreamChatWithToolsKeepsInlineXMLForNonMiMoModel(t *testing.T) {
	xml := "<tool_call><function=tavily__tavily_search><parameter=q>colossus</parameter></function></tool_call>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + xml + `"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "gpt-4o"}, server.Client())

	final, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Search"}}, nil, nil)
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if len(final.ToolCalls) != 0 {
		t.Fatalf("tool calls = %#v, want 0 for non-MiMo model", final.ToolCalls)
	}
	if final.Content != xml {
		t.Fatalf("final content = %q, want unchanged XML", final.Content)
	}
}

func TestClient_StreamChatParsesDataLinesWithoutSpace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data:{\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data:[DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	final, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, func(string) error {
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat() error: %v", err)
	}
	if final != "Hi" {
		t.Fatalf("final = %q, want Hi", final)
	}
}

func TestClient_GenerateTitleUsesNonStreamingRequest(t *testing.T) {
	var gotBody struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %q, want /chat/completions", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": ` "Algebra help" `}},
			},
		})
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	title, err := client.GenerateChatTitle(context.Background(), "Can you explain x?", "Sure.")
	if err != nil {
		t.Fatalf("GenerateChatTitle() error: %v", err)
	}

	if gotBody.Stream {
		t.Fatal("stream = true, want false")
	}
	if gotBody.Model != "mimo" {
		t.Fatalf("model = %q, want mimo", gotBody.Model)
	}
	// The user request and assistant reply are framed into a single user turn as
	// material to be titled, not a turn to answer.
	if len(gotBody.Messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(gotBody.Messages))
	}
	if gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != chatTitleSystemPrompt {
		t.Fatalf("system message = %#v", gotBody.Messages[0])
	}
	if gotBody.Messages[1].Role != "user" || !strings.Contains(gotBody.Messages[1].Content, "Can you explain x?") || !strings.Contains(gotBody.Messages[1].Content, "Sure.") {
		t.Fatalf("user message = %#v", gotBody.Messages[1])
	}
	if title != "Algebra help" {
		t.Fatalf("title = %q, want Algebra help", title)
	}
}

func TestClient_UtilityCallsDisableThinking(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(c *Client) (string, error)
	}{
		{"chat title", func(c *Client) (string, error) { return c.GenerateChatTitle(context.Background(), "Hi", "") }},
		{"reasoning title", func(c *Client) (string, error) {
			return c.GenerateReasoningTitle(context.Background(), "some reasoning")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got struct {
				ReasoningEffort string `json:"reasoning_effort"`
				Thinking        *struct {
					Type string `json:"type"`
				} `json:"thinking"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode: %v", err)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []map[string]any{{"message": map[string]string{"content": "A title"}}},
				})
			}))
			t.Cleanup(server.Close)

			// reasoning_effort high would normally apply to MiMo; utility calls must override it.
			client := NewClient(Config{BaseURL: server.URL, Model: "mimo", ReasoningEffort: "high"}, server.Client())
			if _, err := tc.call(client); err != nil {
				t.Fatalf("call error: %v", err)
			}
			if got.Thinking == nil || got.Thinking.Type != "disabled" {
				t.Fatalf("thinking = %#v, want {disabled}", got.Thinking)
			}
			if got.ReasoningEffort != "" {
				t.Fatalf("reasoning_effort = %q, want empty", got.ReasoningEffort)
			}
		})
	}
}

func TestClient_TitlesSkippedWhenTruncatedAtTokenCap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got struct {
			MaxCompletionTokens int `json:"max_completion_tokens"`
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got.MaxCompletionTokens == 0 {
			t.Fatalf("utility call missing max_completion_tokens cap")
		}
		// Mid-phrase truncation: non-empty content but finish_reason "length".
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]string{"content": "Debugging syntax errors in framew"},
				"finish_reason": "length",
			}},
		})
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	if got, err := client.GenerateReasoningTitle(context.Background(), "some reasoning"); err != nil || got != "" {
		t.Fatalf("reasoning title = %q, err = %v; want skipped (empty)", got, err)
	}
	if got, err := client.GenerateChatTitle(context.Background(), "Hi", ""); err != nil || got != "New chat" {
		t.Fatalf("chat title = %q, err = %v; want New chat", got, err)
	}
}

func TestCleanTitlesStripTrailingDot(t *testing.T) {
	if got := cleanChatTitle("Blue sky explanation."); got != "Blue sky explanation" {
		t.Fatalf("cleanChatTitle trailing dot = %q", got)
	}
	if got := cleanReasoningTitle("Contrasting TCP and UDP protocols."); got != "Contrasting TCP and UDP protocols" {
		t.Fatalf("cleanReasoningTitle trailing dot = %q", got)
	}
	if got := cleanChatTitle("Done..."); got != "Done" {
		t.Fatalf("cleanChatTitle ellipsis = %q", got)
	}
}

func TestClient_StreamChatReturnsErrorForHTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"upstream failed"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	_, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, func(string) error {
		return nil
	})
	if err == nil {
		t.Fatal("StreamChat() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %q, want status 500", err.Error())
	}
}

func TestClient_StreamChatPropagatesDeltaCallbackError(t *testing.T) {
	sentinel := errors.New("sentinel callback error")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	_, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, func(string) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("StreamChat() error = %v, want sentinel", err)
	}
}

func TestClient_GenerateTitleOmitsEmptyAssistantMessage(t *testing.T) {
	var gotMessages []Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		gotMessages = body.Messages
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Greeting"}}]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	title, err := client.GenerateChatTitle(context.Background(), "Hi", "")
	if err != nil {
		t.Fatalf("GenerateChatTitle() error: %v", err)
	}
	if title != "Greeting" {
		t.Fatalf("title = %q, want Greeting", title)
	}
	if len(gotMessages) != 2 {
		t.Fatalf("messages = %#v, want only system and user message", gotMessages)
	}
	for _, message := range gotMessages {
		if message.Role == "assistant" {
			t.Fatalf("messages include contentless assistant message: %#v", gotMessages)
		}
	}
}

func TestClient_GenerateTitleFallsBackForAnswerLikeCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"content": `I don't have specific information about a product called "Lens" by IPverse in my training data.`,
					},
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	title, err := client.GenerateChatTitle(context.Background(), `Tell me about "Lens" by IPverse`, "")
	if err != nil {
		t.Fatalf("GenerateChatTitle() error: %v", err)
	}
	if title != "New chat" {
		t.Fatalf("title = %q, want New chat", title)
	}
}

func TestCleanTitleFallsBackForRefusalLikeCompletion(t *testing.T) {
	for _, raw := range []string{
		"I appreciate the fun and creative idea! Unfortunately, I'm a text-based AI assistant.",
		"Unfortunately, I can't create images directly.",
		"As a text-based AI assistant, I cannot generate images.",
	} {
		t.Run(raw, func(t *testing.T) {
			title := cleanChatTitle(raw)
			if title != "New chat" {
				t.Fatalf("title = %q, want New chat", title)
			}
		})
	}
}

func TestCleanTitleRewritesFirstPersonCreationSentence(t *testing.T) {
	title := cleanChatTitle(`"I'll create a photorealistic image of a male Maine Coon cat for you."`)

	if title != "Creation of a photorealistic image of a male Maine Coon cat" {
		t.Fatalf("title = %q, want passive creation title", title)
	}
}

func TestClient_GenerateTitleFramesAssistantReplyWhenPresent(t *testing.T) {
	var gotMessages []Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		gotMessages = body.Messages
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Greeting"}}]}`))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Model: "mimo"}, server.Client())

	if _, err := client.GenerateChatTitle(context.Background(), "Hi", "Hi there"); err != nil {
		t.Fatalf("GenerateChatTitle() error: %v", err)
	}
	// Both the user request and the assistant reply are framed into one user turn
	// (no separate assistant-role message that the model might continue answering).
	if len(gotMessages) != 2 {
		t.Fatalf("messages = %#v, want system and framed user message", gotMessages)
	}
	if gotMessages[1].Role != "user" || !strings.Contains(gotMessages[1].Content, "Hi there") {
		t.Fatalf("framed user message = %#v, want assistant reply included", gotMessages[1])
	}
}
