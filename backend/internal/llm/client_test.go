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
	if gotBody.Model != textModel {
		t.Fatalf("model = %q, want %q", gotBody.Model, textModel)
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

func TestClient_StreamChatSendsMultimodalContentParts(t *testing.T) {
	var gotBody struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())
	_, err := client.StreamChatResult(context.Background(), []Message{{
		Role: "user",
		ContentParts: []MessageContentPart{
			{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,abc"}},
			{Type: "text", Text: "What is in this image?"},
		},
	}}, nil)
	if err != nil {
		t.Fatalf("StreamChatResult() error: %v", err)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Role != "user" {
		t.Fatalf("messages = %#v, want one user message", gotBody.Messages)
	}
	var parts []MessageContentPart
	if err := json.Unmarshal(gotBody.Messages[0].Content, &parts); err != nil {
		t.Fatalf("unmarshal content parts: %v; raw=%s", err, gotBody.Messages[0].Content)
	}
	if len(parts) != 2 || parts[0].ImageURL == nil || parts[0].ImageURL.URL != "data:image/png;base64,abc" || parts[1].Text != "What is in this image?" {
		t.Fatalf("content parts = %#v, want image_url then text", parts)
	}
}

func TestClient_RoutesImageTurnsToVisionModelAndTextTurnsToTextModel(t *testing.T) {
	for _, tc := range []struct {
		name      string
		messages  []Message
		wantModel string
	}{
		{
			name:      "text-only turn uses the text model",
			messages:  []Message{{Role: "user", Content: "Hi"}},
			wantModel: textModel,
		},
		{
			name: "turn with an image part uses the vision model",
			messages: []Message{{
				Role: "user",
				ContentParts: []MessageContentPart{
					{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,abc"}},
					{Type: "text", Text: "What is this?"},
				},
			}},
			wantModel: visionModel,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotModel string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("Decode request body: %v", err)
				}
				gotModel = body.Model
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			t.Cleanup(server.Close)

			client := NewClient(Config{BaseURL: server.URL}, server.Client())
			result, err := client.StreamChatWithTools(context.Background(), tc.messages, nil, func(StreamEvent) error { return nil })
			if err != nil {
				t.Fatalf("StreamChatWithTools() error: %v", err)
			}
			if gotModel != tc.wantModel {
				t.Fatalf("request model = %q, want %q", gotModel, tc.wantModel)
			}
			// The persisted/observed model must reflect the model that actually ran.
			if result.Model != tc.wantModel {
				t.Fatalf("result.Model = %q, want %q", result.Model, tc.wantModel)
			}
		})
	}
}

// The core invariant: no image_url part may ever be sent to the text-only model
// (mimo-v2.5-pro 404s on image input). Any message carrying an image part must
// route to the vision model.
func TestClient_NeverSendsImagePartsToTextModel(t *testing.T) {
	var gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("Decode request body: %v", err)
		}
		gotModel = body.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL}, server.Client())
	// A multi-message history where only a prior turn carries an image still routes
	// to vision so the image part is never delivered to the text model.
	messages := []Message{
		{Role: "user", ContentParts: []MessageContentPart{
			{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,abc"}},
			{Type: "text", Text: "look"},
		}},
		{Role: "assistant", Content: "I see a cat."},
		{Role: "user", Content: "thanks"},
	}
	if _, err := client.StreamChatWithTools(context.Background(), messages, nil, func(StreamEvent) error { return nil }); err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if gotModel != visionModel {
		t.Fatalf("request model = %q, want %q (image part present)", gotModel, visionModel)
	}
}

// The stream can finalize two ways: on a literal `data: [DONE]` or when the
// connection just ends (EOF) with no terminator. result.Model must reflect the
// routed model on BOTH paths — this exercises the EOF path (no [DONE]) for an
// image turn, which a per-path field swap can silently miss.
func TestClient_ImageTurnReportsVisionModelOnEOFFinishPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Content only, then the handler returns → connection closes with no
		// `data: [DONE]`, forcing the post-loop EOF finalization.
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL}, server.Client())
	result, err := client.StreamChatWithTools(context.Background(), []Message{{
		Role: "user",
		ContentParts: []MessageContentPart{
			{Type: "image_url", ImageURL: &MessageImageURL{URL: "data:image/png;base64,abc"}},
			{Type: "text", Text: "What is this?"},
		},
	}}, nil, func(StreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if result.Model != visionModel {
		t.Fatalf("result.Model = %q, want %q on the EOF finish path", result.Model, visionModel)
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

	client := NewClient(Config{BaseURL: server.URL, MaxCompletionTokens: 4096}, server.Client())
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

	client := NewClient(Config{BaseURL: server.URL, Timeout: 10 * time.Millisecond}, server.Client())
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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

	result, err := client.StreamChatResult(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("StreamChatResult() error: %v", err)
	}
	if result.Model != textModel {
		t.Fatalf("model = %q, want %q", result.Model, textModel)
	}
	if result.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort = %q, want high", result.ReasoningEffort)
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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

	client := NewClient(Config{BaseURL: server.URL, ResponseLogDir: logDir}, server.Client())

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

func TestClient_StreamChatSendsHardcodedReasoningEffort(t *testing.T) {
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

	// Reasoning effort is no longer configurable: MiMo is hardcoded to "high".
	client := NewClient(Config{BaseURL: server.URL}, server.Client())

	if _, err := client.StreamChat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil); err != nil {
		t.Fatalf("StreamChat() error: %v", err)
	}
	if gotBody.ReasoningEffort != "high" {
		t.Fatalf("reasoning_effort = %q, want high", gotBody.ReasoningEffort)
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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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
	if gotBody.MaxCompletionTokens != documentToolMaxCompletionTokens {
		t.Fatalf("max_completion_tokens = %d, want %d", gotBody.MaxCompletionTokens, documentToolMaxCompletionTokens)
	}
}

func TestClient_StreamChatWithDocumentToolUsesExpandedTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Done\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL, Timeout: 10 * time.Millisecond}, server.Client())

	final, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Make a PDF"}}, []Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "create_pdf_file",
			Description: "Create a PDF",
			Parameters:  map[string]any{"type": "object"},
		},
	}}, nil)
	if err != nil {
		t.Fatalf("StreamChatWithTools() error = %v, want document tool timeout expansion", err)
	}
	if final.Content != "Done" {
		t.Fatalf("final content = %q, want Done", final.Content)
	}
}

func TestClient_DocumentToolPreservesDisabledTimeout(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://example.test"}, nil)

	got := client.timeoutForTools([]Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "create_pdf_file",
			Description: "Create a PDF",
			Parameters:  map[string]any{"type": "object"},
		},
	}})
	if got != 0 {
		t.Fatalf("timeoutForTools() = %s, want 0 for disabled timeout", got)
	}
}

func TestClient_NonDocumentToolKeepsConfiguredTimeout(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://example.test", Timeout: 10 * time.Millisecond}, nil)

	got := client.timeoutForTools([]Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "search__web",
			Description: "Search",
			Parameters:  map[string]any{"type": "object"},
		},
	}})
	if got != 10*time.Millisecond {
		t.Fatalf("timeoutForTools() = %s, want 10ms", got)
	}
}

func TestClient_DocumentToolWidensIdleTimeout(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://example.test", IdleTimeout: 60 * time.Second}, nil)

	got := client.toolCallIdleTimeout([]Tool{{
		Type:     "function",
		Function: ToolFunction{Name: "create_pdf_file", Parameters: map[string]any{"type": "object"}},
	}})
	if got != documentToolTimeout {
		t.Fatalf("toolCallIdleTimeout() = %s, want documentToolTimeout %s", got, documentToolTimeout)
	}
}

func TestClient_NonDocumentToolKeepsConfiguredIdleTimeout(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://example.test", IdleTimeout: 60 * time.Second}, nil)

	got := client.toolCallIdleTimeout([]Tool{{
		Type:     "function",
		Function: ToolFunction{Name: "search__web", Parameters: map[string]any{"type": "object"}},
	}})
	if got != 60*time.Second {
		t.Fatalf("toolCallIdleTimeout() = %s, want 60s", got)
	}
}

func TestClient_DisabledIdleWatchdogStaysDisabledForDocumentTool(t *testing.T) {
	client := NewClient(Config{BaseURL: "http://example.test"}, nil)

	got := client.toolCallIdleTimeout([]Tool{{
		Type:     "function",
		Function: ToolFunction{Name: "create_pdf_file", Parameters: map[string]any{"type": "object"}},
	}})
	if got != 0 {
		t.Fatalf("toolCallIdleTimeout() = %s, want 0 when watchdog disabled", got)
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
			configuredTokens:     documentToolMaxCompletionTokens * 2,
			toolName:             "create_pdf_file",
			wantCompletionTokens: documentToolMaxCompletionTokens * 2,
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

			client := NewClient(Config{BaseURL: server.URL, MaxCompletionTokens: tt.configuredTokens}, server.Client())

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
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"lume\"}"}}]}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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
	if call.ID != "call_1" || call.Function.Name != "search__web" || call.Function.Arguments != `{"q":"lume"}` {
		t.Fatalf("tool call = %#v", call)
	}
	// Three events, in order: (1) a pending signal the moment the first tool-call
	// chunk arrives; (2) the tool name surfaced early (under the real id, no argument
	// yet) so the client can show the running tool during the argument gap; (3) the
	// fully-reconstructed call at end-of-stream, same id, updating that same entry.
	if len(events) != 3 || !events[0].ToolPending {
		t.Fatalf("events = %#v, want a tool-pending event first", events)
	}
	if events[1].ToolPending || events[1].ToolCall.ID != "call_1" ||
		events[1].ToolCall.Function.Name != "search__web" || events[1].ToolCall.Function.Arguments != "" {
		t.Fatalf("events = %#v, want early name-only tool call", events)
	}
	if events[2].ToolPending || events[2].ToolCall.ID != "call_1" ||
		events[2].ToolCall.Function.Name != "search__web" || events[2].ToolCall.Function.Arguments != `{"q":"lume"}` {
		t.Fatalf("events = %#v, want final full tool call event", events)
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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

func TestClient_StreamChatWithoutToolsStripsInlineXML(t *testing.T) {
	// The forced tool-free final-answer call regularly gets an inline tool call back
	// instead of an answer. The raw <tool_call> markup must be stripped from the
	// content (never surfaced verbatim) even with no tools offered; the resulting
	// empty answer is the caller's concern (retry + fallback).
	xml := "<tool_call><function=tavily__tavily_search><parameter=q>colossus</parameter></function></tool_call>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + xml + `"}}]}` + "\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

	final, err := client.StreamChatWithTools(context.Background(), []Message{{Role: "user", Content: "Answer"}}, nil, nil)
	if err != nil {
		t.Fatalf("StreamChatWithTools() error: %v", err)
	}
	if strings.Contains(final.Content, "<tool_call>") {
		t.Fatalf("final content = %q, want inline XML stripped (not surfaced verbatim)", final.Content)
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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

func TestClient_StreamChatParsesDataLinesWithoutSpace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data:{\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n"))
		_, _ = w.Write([]byte("data:[DONE]\n\n"))
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

	title, err := client.GenerateThreadTitle(context.Background(), "Can you explain x?", "Sure.", "")
	if err != nil {
		t.Fatalf("GenerateThreadTitle() error: %v", err)
	}

	if gotBody.Stream {
		t.Fatal("stream = true, want false")
	}
	if gotBody.Model != textModel {
		t.Fatalf("model = %q, want %q", gotBody.Model, textModel)
	}
	// The user request and assistant reply are framed into a single user turn as
	// material to be titled, not a turn to answer.
	if len(gotBody.Messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(gotBody.Messages))
	}
	if gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != threadTitleSystemPrompt {
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
		{"chat title", func(c *Client) (string, error) { return c.GenerateThreadTitle(context.Background(), "Hi", "", "") }},
		{"classify", func(c *Client) (string, error) { return c.ClassifyThread(context.Background(), "Hi") }},
		{"reasoning title", func(c *Client) (string, error) {
			return c.GenerateReasoningTitle(context.Background(), "some reasoning", "")
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
			client := NewClient(Config{BaseURL: server.URL}, server.Client())
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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

	if got, err := client.GenerateReasoningTitle(context.Background(), "some reasoning", ""); err != nil || got != "" {
		t.Fatalf("reasoning title = %q, err = %v; want skipped (empty)", got, err)
	}
	if got, err := client.GenerateThreadTitle(context.Background(), "Hi", "", ""); err != nil || got != "New thread" {
		t.Fatalf("thread title = %q, err = %v; want New thread", got, err)
	}
}

func TestClient_ClassifyThreadParsesReply(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{"bare value", "coding", "coding"},
		{"quoted", `"coding"`, "coding"},
		{"trailing punctuation", "coding.", "coding"},
		{"surrounding prose", "The category is coding.", "coding"},
		{"unknown value falls back", "nonsense", "general"},
		{"empty falls back", "", "general"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []map[string]any{{"message": map[string]string{"content": tc.content}}},
				})
			}))
			t.Cleanup(server.Close)

			client := NewClient(Config{BaseURL: server.URL}, server.Client())
			got, err := client.ClassifyThread(context.Background(), "Write a Go function")
			if err != nil {
				t.Fatalf("ClassifyThread() error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("category = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCleanTitlesStripTrailingDot(t *testing.T) {
	if got := cleanThreadTitle("Blue sky explanation."); got != "Blue sky explanation" {
		t.Fatalf("cleanThreadTitle trailing dot = %q", got)
	}
	if got := cleanReasoningTitle("Contrasting TCP and UDP protocols."); got != "Contrasting TCP and UDP protocols" {
		t.Fatalf("cleanReasoningTitle trailing dot = %q", got)
	}
	if got := cleanThreadTitle("Done..."); got != "Done" {
		t.Fatalf("cleanThreadTitle ellipsis = %q", got)
	}
}

func TestCleanChatTitleQuoteHandling(t *testing.T) {
	cases := map[string]string{
		// Real bug: straight opening quote, typographic closing quote. The
		// opening quote must survive and the closing one is normalized to ASCII.
		"\"Healing” by Evanescence": `"Healing" by Evanescence`,
		// Song name quoted with curly quotes, plus trailing words.
		"“Healing” by Evanescence": `"Healing" by Evanescence`,
		// Fully wrapped title still loses its wrapping quotes (no regression).
		`"Blue Sky Explanation"`: "Blue Sky Explanation",
		// Fully wrapped with curly quotes is unwrapped too.
		"“Blue Sky Explanation”": "Blue Sky Explanation",
		// Plain title is untouched.
		"Blue Sky Explanation": "Blue Sky Explanation",
	}
	for in, want := range cases {
		if got := cleanThreadTitle(in); got != want {
			t.Errorf("cleanThreadTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClient_StreamChatReturnsErrorForHTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"upstream failed"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

	title, err := client.GenerateThreadTitle(context.Background(), "Hi", "", "")
	if err != nil {
		t.Fatalf("GenerateThreadTitle() error: %v", err)
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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

	title, err := client.GenerateThreadTitle(context.Background(), `Tell me about "Lens" by IPverse`, "", "")
	if err != nil {
		t.Fatalf("GenerateThreadTitle() error: %v", err)
	}
	if title != "New thread" {
		t.Fatalf("title = %q, want New thread", title)
	}
}

func TestCleanTitleFallsBackForRefusalLikeCompletion(t *testing.T) {
	for _, raw := range []string{
		"I appreciate the fun and creative idea! Unfortunately, I'm a text-based AI assistant.",
		"Unfortunately, I can't create images directly.",
		"As a text-based AI assistant, I cannot generate images.",
	} {
		t.Run(raw, func(t *testing.T) {
			title := cleanThreadTitle(raw)
			if title != "New thread" {
				t.Fatalf("title = %q, want New thread", title)
			}
		})
	}
}

func TestCleanTitleRewritesFirstPersonCreationSentence(t *testing.T) {
	title := cleanThreadTitle(`"I'll create a photorealistic image of a male Maine Coon cat for you."`)

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

	client := NewClient(Config{BaseURL: server.URL}, server.Client())

	if _, err := client.GenerateThreadTitle(context.Background(), "Hi", "Hi there", ""); err != nil {
		t.Fatalf("GenerateThreadTitle() error: %v", err)
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

func TestClient_GenerateTitleHonorsResponseLanguage(t *testing.T) {
	captureSystem := func(t *testing.T, responseLanguage string) string {
		t.Helper()
		var gotSystem string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Messages []Message `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("Decode request body: %v", err)
			}
			if len(body.Messages) > 0 {
				gotSystem = body.Messages[0].Content
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Titel"}}]}`))
		}))
		t.Cleanup(server.Close)

		client := NewClient(Config{BaseURL: server.URL}, server.Client())
		if _, err := client.GenerateThreadTitle(context.Background(), "Hallo", "", responseLanguage); err != nil {
			t.Fatalf("GenerateThreadTitle() error: %v", err)
		}
		return gotSystem
	}

	// A resolved language name appears as a directive in the title system prompt.
	if got := captureSystem(t, "German"); !strings.Contains(got, "this language: German.") {
		t.Fatalf("system prompt = %q, want German directive", got)
	}
	// The English default passes no language, so no directive is appended.
	if got := captureSystem(t, ""); strings.Contains(got, "this language:") {
		t.Fatalf("system prompt = %q, want no language directive for default", got)
	}
}
