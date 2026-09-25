package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/trick77/loom/internal/chat"
)

// An oversized JSON body is a distinct client error, not a generic "invalid
// request body": the client can act on a 413 (shorten, split) but not on a 400
// that looks like a malformed payload.
func TestDecodeJSONBodyReturns413WhenBodyExceedsLimit(t *testing.T) {
	store := &fakeThreadStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "T"}}
	srv := newAuthenticatedServer(t, Deps{Thread: store})
	body := `{"title":"` + strings.Repeat("x", maxJSONBodyBytes+1024) + `"}`
	req := authenticatedRequest(http.MethodPatch, "/api/threads/thr_1", body)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"request body too large"}` {
		t.Fatalf("body = %s, want request body too large", got)
	}
}

// The stream endpoint's limit must leave room for what one send legitimately
// carries: the content cap plus the pasted blocks, which duplicate that text.
func TestStreamMessageAcceptsLargePastedTextBody(t *testing.T) {
	store := &fakeThreadStore{thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "T"}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}})
	pasted := strings.Repeat("p", 3*maxJSONBodyBytes)
	body := `{"content":"` + strings.Repeat("c", 20_000) + `","pastedTexts":[{"text":"` + pasted + `","lineCount":1}]}`
	req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", body)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "event: user_message") {
		t.Fatalf("stream did not start; body: %s", rec.Body.String())
	}
}

// Every JSON body is bounded, the memory instruction included.
func TestEditMemoryRejectsOversizedBody(t *testing.T) {
	store := &fakeThreadStore{
		project:       chat.Project{ID: "proj_1", UserID: testUser.ID, Name: "P"},
		projectMemory: chat.ProjectMemory{ProjectID: "proj_1", Content: "- x", SourceMessageCount: 1},
	}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{editedMemory: "- y"}})
	body := `{"instruction":"` + strings.Repeat("i", maxJSONBodyBytes+1024) + `"}`
	req := authenticatedRequest(http.MethodPost, "/api/projects/proj_1/memory:edit", body)
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body: %s", rec.Code, rec.Body.String())
	}
}

func TestWriteStoreErrorMapsValidationNotFoundAndInternal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{"validation", &chat.ValidationError{Msg: "thread title is required"}, http.StatusBadRequest, `{"error":"thread title is required"}`},
		{"wrapped validation", fmt.Errorf("update: %w", &chat.ValidationError{Msg: "project name is too long"}), http.StatusBadRequest, `{"error":"project name is too long"}`},
		{"project missing", chat.ErrProjectNotFound, http.StatusNotFound, `{"error":"project not found"}`},
		{"thread missing", fmt.Errorf("move: %w", chat.ErrThreadNotFound), http.StatusNotFound, `{"error":"thread not found"}`},
		{"internal", errors.New("disk on fire"), http.StatusInternalServerError, `{"error":"thread store failed"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeStoreError(rec, httptest.NewRequest(http.MethodPost, "/x", nil), tc.err)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tc.body {
				t.Fatalf("body = %s, want %s", got, tc.body)
			}
		})
	}
}
