package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// modelRecorder captures the model each request asked for, so the tests below
// pin WHICH calls moved to the non-Pro deployment and which deliberately did not.
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

func TestShortGatesRunOnTheNonReasoningModel(t *testing.T) {
	server, models := modelRecorder(t, `{"choices":[{"message":{"role":"assistant","content":"{\"action\":\"none\",\"needs_text\":false}"},"finish_reason":"stop"}]}`)
	client := NewClient(Config{BaseURL: server.URL}, server.Client())

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
		if model != nonReasoningModel {
			t.Fatalf("request %d used model %q, want %q", i, model, nonReasoningModel)
		}
	}
}

// TestLongFormHelpersStayOnThePro guards the line the switch must not cross: a
// helper that writes user-visible prose keeps the Pro model even though its
// thinking is disabled too. "No reasoning needed" is the bar, not "thinking is
// off" — the forced final answer disables thinking as well and must never be
// downgraded by widening this routing.
func TestLongFormHelpersStayOnThePro(t *testing.T) {
	server, models := modelRecorder(t, `{"choices":[{"message":{"role":"assistant","content":"a description"},"finish_reason":"stop"}]}`)
	client := NewClient(Config{BaseURL: server.URL}, server.Client())

	if _, err := client.GenerateProjectDescription(context.Background(), "Project", []string{"a thread title"}, ""); err != nil {
		t.Fatalf("GenerateProjectDescription() error: %v", err)
	}

	if len(*models) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(*models))
	}
	if (*models)[0] != textModel {
		t.Fatalf("project description used %q, want the Pro model %q", (*models)[0], textModel)
	}
}
