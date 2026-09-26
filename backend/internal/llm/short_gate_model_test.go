package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/llmwire/llmwiretest"
)

// modelRecorder captures the model each request asked for, so the tests below
// pin WHICH calls route to the short-gate model and which deliberately do not.
func modelRecorder(t *testing.T, body string) (*httptest.Server, *[]string) {
	t.Helper()
	var models []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var decoded struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&decoded)
		models = append(models, decoded.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &models
}

func TestShortGatesRunOnTheShortGateModel(t *testing.T) {
	server, models := modelRecorder(t, `{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"none\",\"needs_text\":false}"},"finish_reason":"stop"}]}`)
	// A gate model distinct from the chat model, so the test can tell them
	// apart. The gate role needs no tools, which this synthetic model lacks.
	client := mustClient(t, Config{BaseURL: server.URL, Models: Roles{Chat: testModel, Gate: llmwiretest.BudgetModel}}, server.Client())

	if _, err := client.ClassifyImageIntent(context.Background(), "hello", false, false); err != nil {
		t.Fatalf("ClassifyImageIntent() error: %v", err)
	}
	if _, err := client.ClassifyThread(context.Background(), "hello"); err != nil {
		t.Fatalf("ClassifyThread() error: %v", err)
	}
	if _, err := client.GenerateReasoningTitle(context.Background(), "hello", ""); err != nil {
		t.Fatalf("GenerateReasoningTitle() error: %v", err)
	}

	if len(*models) != 3 {
		t.Fatalf("recorded %d requests, want 3", len(*models))
	}
	for i, model := range *models {
		if model != llmwiretest.BudgetModel {
			t.Fatalf("request %d used model %q, want the gate model %q", i, model, llmwiretest.BudgetModel)
		}
	}
}

// TestLongFormHelpersRunOnTheChatModel guards the line the gate routing must
// not cross: a helper that writes prose a reader keeps runs on the chat model,
// not the gate model. The bar is what the call produces — a label or an id,
// not prose.
func TestLongFormHelpersRunOnTheChatModel(t *testing.T) {
	server, models := modelRecorder(t, `{"choices":[{"message":{"role":"assistant","content":"a description"},"finish_reason":"stop"}]}`)
	client := mustClient(t, Config{BaseURL: server.URL, Models: Roles{Chat: testModel, Gate: llmwiretest.BudgetModel}}, server.Client())

	if _, err := client.GenerateProjectDescription(context.Background(), "Project", []string{"a thread title"}, ""); err != nil {
		t.Fatalf("GenerateProjectDescription() error: %v", err)
	}

	if len(*models) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(*models))
	}
	if (*models)[0] != testModel {
		t.Fatalf("project description used %q, want the chat model %q", (*models)[0], testModel)
	}
}
