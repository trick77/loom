package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"context"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// TestRefreshProjectDescription_GeneratedThroughRealClient is an end-to-end guard for
// the description path: it drives refreshProjectDescriptionIfDue with the REAL
// *llm.Client against a fake upstream, proving the big-picture description is generated
// from the project's thread titles and persisted. It also enforces max_completion_tokens
// so a too-small cap would truncate to finish_reason=length — the description still
// persists because it carries its own larger budget and salvages truncation.
func TestRefreshProjectDescription_GeneratedThroughRealClient(t *testing.T) {
	const wantDescription = "Plans a week-long trip across Japan covering Kyoto stays, food, and day trips."

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			MaxCompletionTokens int `json:"max_completion_tokens"`
		}
		_ = json.Unmarshal(body, &req)

		// A ≤160-char fragment realistically needs ~40+ completion tokens — more than the
		// old 32-token title cap, which is exactly why the description used to truncate.
		const descriptionTokensNeeded = 45
		content, finish := wantDescription, "stop"
		if req.MaxCompletionTokens < descriptionTokensNeeded {
			content, finish = "Plans a week-long", "length"
		}
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": finish,
			}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "k"}, srv.Client())

	projectID := "proj_japan"
	store := &fakeThreadStore{
		project:             chat.Project{ID: projectID, UserID: testUser.ID, Name: "Japan Trip", Description: ""},
		projectThreadTitles: []string{"Where to stay in Kyoto", "Day trips from Osaka", "Best ramen spots"},
	}
	s := &server{thread: store, llm: client}

	if err := s.refreshProjectDescriptionIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectDescriptionIfDue() error: %v", err)
	}

	if !store.projectDescriptionChanged {
		t.Fatal("project description was not persisted")
	}
	if store.project.Description != wantDescription {
		t.Fatalf("description = %q, want %q", store.project.Description, wantDescription)
	}
	if store.project.DescriptionSourceThreadCount != 3 {
		t.Fatalf("DescriptionSourceThreadCount = %d, want 3 (the titled-thread count)", store.project.DescriptionSourceThreadCount)
	}
}
