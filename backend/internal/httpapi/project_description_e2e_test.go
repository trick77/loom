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

// TestRefreshProjectMemory_DescriptionGeneratedThroughRealClient is an end-to-end guard
// for the regression where the auto project description, after it was moved onto the
// project-memory refresh, stayed empty forever: it shared the 32-token title cap, so the
// reply was truncated (finish_reason=length) and discarded.
//
// It drives the real refresh path (refreshProjectMemoryIfDue -> refreshMemory -> describe
// + memory generation) with the REAL *llm.Client against a fake upstream that ENFORCES
// max_completion_tokens — returning finish_reason=length when the budget is too small for
// the canned description. So this passes only because the description now requests its own
// larger budget; under the old 32-token cap the upstream would truncate it to empty and
// the description would never persist.
func TestRefreshProjectMemory_DescriptionGeneratedThroughRealClient(t *testing.T) {
	const wantDescription = "Plans a week-long trip across Japan covering Kyoto stays, food, and day trips."
	const memoryContent = "Trip to Japan; focus Kyoto."

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			MaxCompletionTokens int `json:"max_completion_tokens"`
		}
		_ = json.Unmarshal(body, &req)

		// Memory generation requests 768 tokens; the description requests far less, so
		// a 512 midpoint cleanly tells the two refresh sub-calls apart.
		// A ≤160-char fragment realistically needs ~40+ completion tokens — more than the
		// old 32-token title cap, which is exactly why the description used to truncate.
		const descriptionTokensNeeded = 45
		content, finish := memoryContent, "stop"
		if req.MaxCompletionTokens <= 512 {
			// description call: enforce the budget — too small a cap truncates to length.
			if req.MaxCompletionTokens < descriptionTokensNeeded {
				content, finish = "Plans a week-long", "length"
			} else {
				content, finish = wantDescription, "stop"
			}
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
		projectMessageCount: 2,
		messages: []chat.Message{
			{Role: chat.RoleUser, Content: "Where should we stay in Kyoto?"},
			{Role: chat.RoleAssistant, Content: "Gion is central and walkable."},
		},
	}
	s := &server{thread: store, llm: client}

	if err := s.refreshProjectMemoryIfDue(context.Background(), testUser, projectID); err != nil {
		t.Fatalf("refreshProjectMemoryIfDue() error: %v", err)
	}

	if !store.projectDescriptionChanged {
		t.Fatal("project description was not persisted (regression: truncated/discarded)")
	}
	if store.project.Description != wantDescription {
		t.Fatalf("description = %q, want %q", store.project.Description, wantDescription)
	}
}
