package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_StreamChatSendsOpenAICompatibleRequest(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Stream   bool      `json:"stream"`
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
	if len(gotBody.Messages) != 1 || gotBody.Messages[0] != (Message{Role: "user", Content: "Hi"}) {
		t.Fatalf("messages = %#v, want user message", gotBody.Messages)
	}
	if strings.Join(chunks, "") != "Hello" {
		t.Fatalf("chunks = %#v, want Hello", chunks)
	}
	if final != "Hello" {
		t.Fatalf("final = %q, want Hello", final)
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
	if gotBody.Messages[0] != (Message{Role: "system", Content: "Name this chat in 2 to 6 words. Return only the title."}) {
		t.Fatalf("system message = %#v", gotBody.Messages[0])
	}
	if gotBody.Messages[1] != (Message{Role: "user", Content: "Can you explain x?"}) {
		t.Fatalf("user message = %#v", gotBody.Messages[1])
	}
	if gotBody.Messages[2] != (Message{Role: "assistant", Content: "Sure."}) {
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
