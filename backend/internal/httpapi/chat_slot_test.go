package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

func deepModelInfo() llm.ModelInfo {
	return llm.ModelInfo{ID: "chat", Slots: []llm.SlotInfo{{Key: llm.SlotStandard, Model: "chat"}, {Key: llm.SlotDeep, Model: "deep"}}}
}

// The slot keys agree across the two packages that spell them.
func TestChatSlotKeysAgree(t *testing.T) {
	if chat.ChatSlotStandard != llm.SlotStandard {
		t.Fatalf("chat %q, llm %q", chat.ChatSlotStandard, llm.SlotStandard)
	}
}

// A thread's slot is switched through PATCH, and only to a configured slot:
// the client names a slot, never a model, and cannot reach one the
// deployment did not configure.
func TestUpdateThreadChatSlot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model llm.ModelInfo
		body  string
		code  int
	}{
		{"deep configured", deepModelInfo(), `{"chatSlot":"deep"}`, http.StatusOK},
		{"back to standard", deepModelInfo(), `{"chatSlot":"standard"}`, http.StatusOK},
		{"deep not configured", llm.ModelInfo{ID: "chat", Slots: []llm.SlotInfo{{Key: llm.SlotStandard}}}, `{"chatSlot":"deep"}`, http.StatusBadRequest},
		{"unknown slot", deepModelInfo(), `{"chatSlot":"claude-model-id"}`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread"}}
			srv := newAuthenticatedServer(t, Deps{Thread: store, Model: tc.model})
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, authenticatedRequest(http.MethodPatch, "/api/threads/thr_1", tc.body))
			if rec.Code != tc.code {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.code, rec.Body.String())
			}
			if tc.code == http.StatusOK && (store.UpdateThreadInput.ChatSlot == nil || store.Thread.ChatSlot == "") {
				t.Fatalf("slot not passed to the store: %+v", store.UpdateThreadInput)
			}
		})
	}
}

// The start page creates the thread with the slot the composer had picked.
func TestCreateThreadChatSlot(t *testing.T) {
	store := &fakeThreadStore{}
	srv := newAuthenticatedServer(t, Deps{Thread: store, Model: deepModelInfo()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads", `{"title":"Hi","chatSlot":"deep"}`))
	if rec.Code != http.StatusCreated || store.CreateThreadInput.ChatSlot != llm.SlotDeep {
		t.Fatalf("status %d, slot %q: %s", rec.Code, store.CreateThreadInput.ChatSlot, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads", `{"title":"Hi","chatSlot":"nope"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown slot: status %d, want 400", rec.Code)
	}
}
