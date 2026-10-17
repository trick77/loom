package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// gatedChatClient holds its first answer call until release is closed, so a
// test can act while a turn is running.
type gatedChatClient struct {
	fakeChatClient
	entered chan struct{}
	release chan struct{}
	calls   *atomic.Int32
}

func newGatedChatClient() gatedChatClient {
	return gatedChatClient{entered: make(chan struct{}), release: make(chan struct{}), calls: &atomic.Int32{}}
}

func (c gatedChatClient) StreamChatWithTools(ctx context.Context, history []llm.Message, tools []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	if c.calls.Add(1) == 1 {
		// Some of the answer streams before the hold, so a stop keeps a
		// partial answer.
		if onEvent != nil {
			if err := onEvent(llm.StreamEvent{Delta: "Partial"}); err != nil {
				return llm.StreamResult{}, err
			}
		}
		close(c.entered)
		select {
		case <-c.release:
		case <-ctx.Done():
			return llm.StreamResult{Content: "Partial"}, ctx.Err()
		}
	}
	return c.fakeChatClient.StreamChatWithTools(ctx, history, tools, onEvent)
}

// startStream runs a send in the background and returns its finished
// recorder on the channel.
func startStream(srv http.Handler, body string) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", body))
		done <- rec
	}()
	return done
}

func waitRecorder(t *testing.T, done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case rec := <-done:
		return rec
	case <-time.After(10 * time.Second):
		t.Fatal("stream did not finish")
		return nil
	}
}

func waitEntered(t *testing.T, c gatedChatClient) {
	t.Helper()
	select {
	case <-c.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("turn never reached the model")
	}
}

func queueRequest(t *testing.T, srv http.Handler, content, sendID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:queue", `{"content":"`+content+`","clientMessageId":"`+sendID+`"}`))
	return rec
}

func TestQueuedMessageRunsAsAFollowUpTurnInTheSameStream(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := newGatedChatClient()
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})

	done := startStream(srv, `{"content":"Hi","clientMessageId":"send-1"}`)
	waitEntered(t, llmClient)
	if rec := queueRequest(t, srv, "And then?", "send-2"); rec.Code != http.StatusAccepted {
		t.Fatalf("queue status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	close(llmClient.release)
	rec := waitRecorder(t, done)

	body := rec.Body.String()
	if got := strings.Count(body, "event: assistant_message"); got != 2 {
		t.Fatalf("assistant_message events = %d, want 2:\n%s", got, body)
	}
	if got := strings.Count(body, "event: done"); got != 1 {
		t.Fatalf("done events = %d, want 1 at the end:\n%s", got, body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: {}") || strings.LastIndex(body, "event: done") < strings.LastIndex(body, "event: assistant_message") {
		t.Fatalf("done must close the stream after the follow-up:\n%s", body)
	}
	if !strings.Contains(body, `"clientMessageId":"send-2"`) {
		t.Fatalf("follow-up user_message missing its send id:\n%s", body)
	}
	var order []string
	for _, m := range store.Messages {
		order = append(order, string(m.Role)+":"+m.Content)
	}
	if strings.Join(order, "|") != "user:Hi|assistant:Hello|user:And then?|assistant:Hello" {
		t.Fatalf("transcript = %v", order)
	}
	if len(store.Queue.Snapshot()) != 0 {
		t.Fatal("queue not emptied")
	}
}

func TestQueueOnAnIdleThreadStartsATurn(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}})

	if rec := queueRequest(t, srv, "Late question", "send-9"); rec.Code != http.StatusAccepted {
		t.Fatalf("queue status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	// The started turn is followed like any other: attach until it ends.
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/threads/thr_1/messages:attach", ""))
	if rec.Code == http.StatusOK && !strings.Contains(rec.Body.String(), "event: done") {
		t.Fatalf("attach body without done:\n%s", rec.Body.String())
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/threads/thr_1/messages:attach", ""))
		if rec.Code == http.StatusNoContent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("started turn never finished")
		}
	}
	if len(store.Messages) != 2 || store.Messages[0].Content != "Late question" || store.Messages[1].Role != chat.RoleAssistant {
		t.Fatalf("messages = %+v, want the queued question answered", store.Messages)
	}
}

func TestStopReturnsQueuedMessagesAndRunsNoFollowUp(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := newGatedChatClient()
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})

	done := startStream(srv, `{"content":"Hi","clientMessageId":"send-1"}`)
	waitEntered(t, llmClient)
	if rec := queueRequest(t, srv, "Never mind", "send-2"); rec.Code != http.StatusAccepted {
		t.Fatalf("queue status = %d", rec.Code)
	}
	stop := httptest.NewRecorder()
	srv.ServeHTTP(stop, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stop?sendId=send-1&source=stop_button", ""))
	if stop.Code != http.StatusOK {
		t.Fatalf("stop status = %d, want 200: %s", stop.Code, stop.Body.String())
	}
	var resp stopStreamResponse
	if err := json.Unmarshal(stop.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Queued) != 1 || resp.Queued[0].Content != "Never mind" {
		t.Fatalf("stop response = %+v, want the queued message back", resp)
	}
	rec := waitRecorder(t, done)
	if strings.Count(rec.Body.String(), "event: user_message") != 1 {
		t.Fatalf("no follow-up expected after stop:\n%s", rec.Body.String())
	}
	if len(store.Queue.Snapshot()) != 0 {
		t.Fatal("queue not drained by stop")
	}
}

// The stopped turn unwinds the moment it is canceled, and its hand-off looks
// at the queue. It must not get in between the stop and the drain: there it
// found the message still queued and started a stream that answered it, and
// the stop handed back nothing (CI: stop status 204).
func TestStopAndDrainKeepsTheHandOffOut(t *testing.T) {
	var reg activeStreamRegistry
	var stream *activeStream
	var queued, handedOff atomic.Bool
	queued.Store(true)
	handOffDone := make(chan struct{})
	// Canceling the turn sets it unwinding to its hand-off, while the stop's
	// goroutine is descheduled for a moment, as on a busy CI runner.
	cancel := func(error) {
		go func() {
			defer close(handOffDone)
			reg.handOff(stream, queued.Load, func() { handedOff.Store(true) })
		}()
		time.Sleep(50 * time.Millisecond)
	}
	stream, unregister := reg.registerStream("u1", "thr_1", "send-1", cancel, newTurnHub())
	defer unregister()
	stopped := reg.stopAndDrain("u1", "thr_1", "send-1", errStreamStopRequested, func() {
		queued.Store(false) // the drain takes the message
	})
	if !stopped {
		t.Fatal("stopAndDrain found no stream")
	}
	<-handOffDone
	if handedOff.Load() {
		t.Fatal("the hand-off answered the queue between the stop and its drain")
	}
}

func TestGetThreadListsQueuedMessagesAndDeleteCancelsOne(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}})
	if _, err := store.EnqueueMessage(context.Background(), testUser.ID, "thr_1", "waiting", nil, "send-5"); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/threads/thr_1", ""))
	if !strings.Contains(rec.Body.String(), `"queuedMessages":[{`) || !strings.Contains(rec.Body.String(), `"clientMessageId":"send-5"`) {
		t.Fatalf("thread response without the queued message:\n%s", rec.Body.String())
	}

	del := httptest.NewRecorder()
	srv.ServeHTTP(del, authenticatedRequest(http.MethodDelete, "/api/threads/thr_1/messages:queue/send-5", ""))
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", del.Code)
	}
	again := httptest.NewRecorder()
	srv.ServeHTTP(again, authenticatedRequest(http.MethodDelete, "/api/threads/thr_1/messages:queue/send-5", ""))
	if again.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", again.Code)
	}
}

func TestQueueRejectsBadRequests(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}})
	for name, body := range map[string]string{
		"no send id":  `{"content":"x"}`,
		"bad send id": `{"content":"x","clientMessageId":"a b"}`,
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:queue", body))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	missing := &fakeThreadStore{}
	srv = newAuthenticatedServer(t, Deps{Thread: missing, LLM: fakeChatClient{}})
	rec := queueRequest(t, srv, "x", "send-1")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown thread: status = %d, want 404", rec.Code)
	}
}

func TestResumeQueuedTurnsAnswersMessagesLeftByARestart(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	if _, err := store.EnqueueMessage(context.Background(), testUser.ID, "thr_1", "left over", nil, "send-7"); err != nil {
		t.Fatal(err)
	}
	s := newServer(Deps{Thread: store, LLM: fakeChatClient{}, Users: fakeUserStore{user: testUser, ok: true}})
	s.resumeQueuedTurns(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	for s.activeStreams.lookup(testUser.ID, "thr_1") != nil || len(store.Queue.Snapshot()) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("resumed turn never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.background.Stop(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if len(store.Messages) != 2 || store.Messages[0].Content != "left over" {
		t.Fatalf("messages = %+v, want the left-over question answered", store.Messages)
	}
}
