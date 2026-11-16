package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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

// A send carries the slot the composer showed and saves it before the turn
// reads the thread: a send right after a switch no longer races the switch's
// own save. An unknown slot fails the send before anything is stored.
func TestStreamMessageSavesItsChatSlot(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread", ChatSlot: llm.SlotStandard}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}, Model: deepModelInfo()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi","chatSlot":"deep"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if store.Thread.ChatSlot != llm.SlotDeep {
		t.Fatalf("thread slot = %q, want deep", store.Thread.ChatSlot)
	}

	before := len(store.Messages)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi","chatSlot":"nope"}`))
	if rec.Code != http.StatusBadRequest || len(store.Messages) != before {
		t.Fatalf("unknown slot: status %d, %d messages stored, want 400 and none", rec.Code, len(store.Messages)-before)
	}
}

// A queued message carries its slot too: the follow-up turn answering it
// reads the thread fresh.
func TestQueueMessageSavesItsChatSlot(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread", ChatSlot: llm.SlotStandard}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}, Model: deepModelInfo()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:queue", `{"content":"Hi","clientMessageId":"send-1","chatSlot":"deep"}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if store.Thread.ChatSlot != llm.SlotDeep {
		t.Fatalf("thread slot = %q, want deep", store.Thread.ChatSlot)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:queue", `{"content":"Hi","clientMessageId":"send-2","chatSlot":"nope"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown slot: status %d, want 400", rec.Code)
	}
	if queued, _ := store.ListQueued(context.Background(), testUser.ID, "thr_1"); slices.ContainsFunc(queued, func(m chat.QueuedMessage) bool { return m.ClientMessageID == "send-2" }) {
		t.Fatal("unknown slot: the message was queued anyway")
	}
}

// A send rejected after its slot was checked (a bad attachment list) leaves
// the thread's slot as it was.
func TestRejectedSendKeepsTheThreadsSlot(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread", ChatSlot: llm.SlotStandard}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}, Model: deepModelInfo()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi","chatSlot":"deep","imageAttachmentIds":["art_missing"]}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if store.Thread.ChatSlot != llm.SlotStandard {
		t.Fatalf("thread slot = %q, want it left standard", store.Thread.ChatSlot)
	}
}

// A slot that cannot be saved fails the send before its message is stored:
// the turn would otherwise run on a slot the user did not pick.
func TestStreamMessageFailsWhenItsSlotCannotBeSaved(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Thread", ChatSlot: llm.SlotStandard}}
	store.UpdateThreadErr = errors.New("disk full")
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}, Model: deepModelInfo()})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi","chatSlot":"deep"}`))
	if rec.Code != http.StatusInternalServerError || len(store.Messages) != 0 {
		t.Fatalf("status %d, %d messages stored, want 500 and none", rec.Code, len(store.Messages))
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
