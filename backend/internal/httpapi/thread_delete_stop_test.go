package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
)

// startBlockedStream kicks off a streaming turn that parks inside the llm client
// until its context is canceled, and returns a channel closed when the stream
// handler has returned.
func startBlockedStream(t *testing.T, srv http.Handler, llmClient *blockingChatClient, threadID string) chan struct{} {
	t.Helper()
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		rec := httptest.NewRecorder()
		req := authenticatedRequest(http.MethodPost, "/api/threads/"+threadID+"/messages:stream", `{"content":"Hi"}`)
		srv.ServeHTTP(rec, req)
	}()
	select {
	case <-llmClient.started:
	case <-time.After(time.Second):
		t.Fatal("stream did not reach llm client")
	}
	return streamDone
}

// Deleting a thread must terminate a turn still generating on it — otherwise the
// handler keeps calling the model and its tools against a thread that no longer
// exists — and must wait for that turn to unwind before removing the rows.
func TestDeleteThreadCancelsActiveAssistantTurn(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &blockingChatClient{started: make(chan struct{}), done: make(chan struct{})}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})
	streamDone := startBlockedStream(t, srv, llmClient, "thr_1")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/threads/thr_1", ""))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-llmClient.done:
	case <-time.After(time.Second):
		t.Fatal("delete did not cancel the llm context")
	}
	if !errors.Is(llmClient.cancelCause, errStreamThreadDeleted) {
		t.Fatalf("cancel cause = %v, want %v", llmClient.cancelCause, errStreamThreadDeleted)
	}
	// The delete waited for the turn, so the handler is already unwinding: a short
	// window is enough. A plain delete without the wait would still have the turn
	// running here.
	select {
	case <-streamDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("delete returned before the stream handler unwound")
	}
}

func TestBulkDeleteThreadsCancelsActiveAssistantTurn(t *testing.T) {
	store := &fakeThreadStore{
		thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"},
	}
	llmClient := &blockingChatClient{started: make(chan struct{}), done: make(chan struct{})}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})
	streamDone := startBlockedStream(t, srv, llmClient, "thr_1")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads:delete", `{"threadIds":["thr_1","thr_2"]}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-llmClient.done:
	case <-time.After(time.Second):
		t.Fatal("bulk delete did not cancel the llm context")
	}
	if !errors.Is(llmClient.cancelCause, errStreamThreadDeleted) {
		t.Fatalf("cancel cause = %v, want %v", llmClient.cancelCause, errStreamThreadDeleted)
	}
	select {
	case <-streamDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("bulk delete returned before the stream handler unwound")
	}
}

// Titling an untitled thread runs inline on the cancel path with a detached
// context. On a delete it is both wasted model tokens (the UpdateThread that
// follows no-ops on the missing thread) and a multi-second stall the deleting
// request would have to wait out — so it must be skipped there, and only there.
func TestDeleteThreadSkipsTitleGenerationForCanceledTurn(t *testing.T) {
	newServer := func(llmClient *blockingChatClient) http.Handler {
		store := &fakeThreadStore{
			thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: chat.DefaultThreadTitle},
		}
		return newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})
	}

	t.Run("delete skips titling", func(t *testing.T) {
		llmClient := &blockingChatClient{
			started:        make(chan struct{}),
			done:           make(chan struct{}),
			partialContent: "Partial answer",
		}
		srv := newServer(llmClient)
		streamDone := startBlockedStream(t, srv, llmClient, "thr_1")

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodDelete, "/api/threads/thr_1", ""))
		<-streamDone

		if calls := llmClient.titleCalls.Load(); calls != 0 {
			t.Fatalf("title calls = %d, want 0 after a delete-canceled turn", calls)
		}
	})

	t.Run("stop still titles", func(t *testing.T) {
		llmClient := &blockingChatClient{
			started:        make(chan struct{}),
			done:           make(chan struct{}),
			partialContent: "Partial answer",
		}
		srv := newServer(llmClient)
		streamDone := startBlockedStream(t, srv, llmClient, "thr_1")

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stop", ""))
		<-streamDone

		if calls := llmClient.titleCalls.Load(); calls != 1 {
			t.Fatalf("title calls = %d, want 1 after a stopped turn", calls)
		}
	})
}

func TestStopAndWaitBlocksUntilTheStreamUnregisters(t *testing.T) {
	var registry activeStreamRegistry
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	unregister := registry.register(testUser.ID, "thr_1", cancel)

	// The handler unwinds shortly after the cancel, as a canceled turn does.
	go func() {
		<-ctx.Done()
		unregister()
	}()

	start := time.Now()
	registry.stopAndWait(testUser.ID, "thr_1", errStreamThreadDeleted, time.Second)

	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("stopAndWait took %s, want a return on unregister rather than the timeout", elapsed)
	}
	if !errors.Is(context.Cause(ctx), errStreamThreadDeleted) {
		t.Fatalf("cancel cause = %v, want %v", context.Cause(ctx), errStreamThreadDeleted)
	}
}

// A turn that ignores its cancellation must not hold the delete open forever.
func TestStopAndWaitGivesUpAfterTheTimeout(t *testing.T) {
	var registry activeStreamRegistry
	// A turn parked on something that ignores its context: nothing ever unregisters.
	unregister := registry.register(testUser.ID, "thr_1", func(error) {})
	defer unregister()

	done := make(chan struct{})
	go func() {
		defer close(done)
		registry.stopAndWait(testUser.ID, "thr_1", errStreamThreadDeleted, 20*time.Millisecond)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stopAndWait did not return after its timeout")
	}
}

// The registry is keyed by (user, thread), so one user's delete can never reach
// another user's stream on a same-id thread.
func TestStopAndWaitIsScopedToTheOwningUser(t *testing.T) {
	var registry activeStreamRegistry
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	unregister := registry.register("user_1", "thr_1", cancel)
	defer unregister()

	registry.stopAndWait("user_2", "thr_1", errStreamThreadDeleted, 20*time.Millisecond)

	if ctx.Err() != nil {
		t.Fatalf("stream of user_1 was canceled by user_2's delete: %v", context.Cause(ctx))
	}
}

func TestStreamCancelDetailsClassifiesThreadDeleted(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(errStreamThreadDeleted)

	source, reason := streamCancelDetails(ctx)

	if source != "thread_deleted" || reason != errStreamThreadDeleted.Error() {
		t.Fatalf("details = %q %q, want thread deleted", source, reason)
	}
}
