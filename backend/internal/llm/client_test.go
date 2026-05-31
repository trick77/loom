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
)

func TestClient_StreamChatSendsOpenAICompatibleRequest(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody struct {
		Model           string    `json:"model"`
		Messages        []Message `json:"messages"`
		Stream          bool      `json:"stream"`
		ReasoningEffort string    `json:"reasoning_effort"`
		StreamOptions   struct {
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

func TestClient_StreamChatWithToolsReconstructsToolCallDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search__web","arguments":"{\"q\""}}]}}]}` + "\n\n"))
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"spark\"}"}}]}}]}` + "\n\n"))
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
	if call.ID != "call_1" || call.Function.Name != "search__web" || call.Function.Arguments != `{"q":"spark"}` {
		t.Fatalf("tool call = %#v", call)
	}
	if len(events) != 1 || events[0].ToolCall.Function.Name != "search__web" {
		t.Fatalf("events = %#v, want final tool call event", events)
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

	title, err := client.GenerateTitle(context.Background(), "Can you explain x?", "Sure.")
	if err != nil {
		t.Fatalf("GenerateTitle() error: %v", err)
	}

	if gotBody.Stream {
		t.Fatal("stream = true, want false")
	}
	if gotBody.Model != "mimo" {
		t.Fatalf("model = %q, want mimo", gotBody.Model)
	}
	if len(gotBody.Messages) != 3 {
		t.Fatalf("len(messages) = %d, want 3", len(gotBody.Messages))
	}
	if gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != "Name this chat in 2 to 6 words. Return only the title." {
		t.Fatalf("system message = %#v", gotBody.Messages[0])
	}
	if gotBody.Messages[1].Role != "user" || gotBody.Messages[1].Content != "Can you explain x?" {
		t.Fatalf("user message = %#v", gotBody.Messages[1])
	}
	if gotBody.Messages[2].Role != "assistant" || gotBody.Messages[2].Content != "Sure." {
		t.Fatalf("assistant message = %#v", gotBody.Messages[2])
	}
	if title != "Algebra help" {
		t.Fatalf("title = %q, want Algebra help", title)
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
