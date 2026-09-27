package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGenerateWorkingTitleFramesTheMessageAndCleansTheTitle(t *testing.T) {
	var userContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, m := range body.Messages {
			if m.Role == "user" {
				userContent = m.Content
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": `"Checking whether 1001 is prime."`}},
			},
		})
	}))
	t.Cleanup(server.Close)
	client := mustClient(t, Config{BaseURL: server.URL}, server.Client())

	got, err := client.GenerateWorkingTitle(context.Background(), "Is 1001 a prime number?", "")
	if err != nil {
		t.Fatalf("GenerateWorkingTitle() error: %v", err)
	}
	if got != "Checking whether 1001 is prime" {
		t.Fatalf("GenerateWorkingTitle() = %q, want the cleaned title", got)
	}
	// Quoted as material to title, not a turn to answer: a bare imperative
	// ("Explain X") makes a model answer it instead.
	if !strings.Contains(userContent, "\"\"\"\nIs 1001 a prime number?\n\"\"\"") {
		t.Fatalf("user content = %q, want the message quoted", userContent)
	}
}

func TestGenerateWorkingTitleSkipsAnEmptyMessage(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]string{"content": "Anything"}}}})
	}))
	t.Cleanup(server.Close)
	client := mustClient(t, Config{BaseURL: server.URL}, server.Client())

	got, err := client.GenerateWorkingTitle(context.Background(), "   ", "")
	if err != nil || got != "" {
		t.Fatalf("GenerateWorkingTitle() = %q, %v; want skipped", got, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("model calls = %d, want none for an empty message", calls.Load())
	}
}

func TestGenerateWorkingTitleRejectsScriptDrift(t *testing.T) {
	client := titleServer(t, "检查1001是否为质数")
	got, err := client.GenerateWorkingTitle(context.Background(), "Is 1001 a prime number?", "")
	if err != nil {
		t.Fatalf("GenerateWorkingTitle() error: %v", err)
	}
	if got != "" {
		t.Fatalf("GenerateWorkingTitle() = %q, want drift rejected", got)
	}
}
