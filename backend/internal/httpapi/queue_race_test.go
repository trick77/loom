package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/background"
	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/ship/kit/sse"
)

// A message queued after a stop waits until the stopped turn has ended, then
// runs on a stream of its own: never beside the turn still unwinding.
func TestMessageQueuedAfterAStopRunsOnceTheStoppedTurnEnded(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := newGatedChatClient()
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})

	done := startStream(srv, `{"content":"Hi","clientMessageId":"send-1"}`)
	waitEntered(t, llmClient)
	stop := httptest.NewRecorder()
	srv.ServeHTTP(stop, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stop?sendId=send-1", ""))
	if stop.Code != http.StatusNoContent {
		t.Fatalf("stop status = %d, want 204", stop.Code)
	}
	if rec := queueRequest(t, srv, "Second", "send-2"); rec.Code != http.StatusAccepted {
		t.Fatalf("queue status = %d", rec.Code)
	}
	waitRecorder(t, done)

	deadline := time.Now().Add(5 * time.Second)
	for {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/threads/thr_1/messages:attach", ""))
		if rec.Code == http.StatusNoContent && len(store.Queue.Snapshot()) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queued message never answered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var order []string
	for _, m := range store.Messages {
		order = append(order, string(m.Role)+":"+m.Content)
	}
	// The stopped turn's partial answer is stored before the message queued
	// after the stop.
	if strings.Join(order, "|") != "user:Hi|assistant:Partial|user:Second|assistant:Hello" {
		t.Fatalf("transcript = %v", order)
	}
}

// A hand-off during shutdown cannot start a stream; it must not hang on the
// queue lock it holds.
func TestHandOffDuringShutdownDoesNotHang(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID}}
	if _, err := store.EnqueueMessage(context.Background(), testUser.ID, "thr_1", "left", nil, "send-1"); err != nil {
		t.Fatal(err)
	}
	bg := background.New(context.Background())
	// Stopped: Spawn refuses every task from here on.
	_ = bg.Stop(0)
	s := newServer(Deps{Thread: store, LLM: fakeChatClient{}, Background: bg})
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	hub := newTurnHub()
	stream, _ := s.activeStreams.registerStream(testUser.ID, "thr_1", "", cancel, hub)
	c := &turnChain{s: s, user: testUser, threadID: "thr_1", stream: stream, hub: hub, liveCtx: ctx, streamCtx: ctx}

	ended := make(chan struct{})
	go func() {
		c.endWith("done", struct{}{})
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("hand-off hung during shutdown")
	}
	if len(store.Queue.Snapshot()) != 1 {
		t.Fatal("the queued message must stay for the next boot")
	}
}

// failingConsumeStore cannot move queued messages into the thread.
type failingConsumeStore struct{ *fakeThreadStore }

func (failingConsumeStore) ConsumeQueued(context.Context, string, string) ([]chat.Message, error) {
	return nil, errors.New("database is locked")
}

func TestQueuedMessagesGoBackWhenTheFollowUpCannotStart(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := newGatedChatClient()
	srv := newAuthenticatedServer(t, Deps{Thread: failingConsumeStore{store}, LLM: llmClient})

	done := startStream(srv, `{"content":"Hi","clientMessageId":"send-1"}`)
	waitEntered(t, llmClient)
	if rec := queueRequest(t, srv, "Second", "send-2"); rec.Code != http.StatusAccepted {
		t.Fatalf("queue status = %d", rec.Code)
	}
	close(llmClient.release)
	body := waitRecorder(t, done).Body.String()

	if !strings.Contains(body, "event: queue_returned") || !strings.Contains(body, `"content":"Second"`) {
		t.Fatalf("queued message not handed back:\n%s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: {}") {
		t.Fatalf("stream must end with done:\n%s", body)
	}
	if len(store.Queue.Snapshot()) != 0 {
		t.Fatal("handed-back message still queued")
	}
}

func TestStopReachesAStreamTheQueueStarted(t *testing.T) {
	var registry activeStreamRegistry
	stopped := make(chan error, 1)
	registry.register("u", "t", "", func(err error) { stopped <- err }, nil)
	if !registry.stop("u", "t", "send-of-an-earlier-turn", errStreamStopRequested) {
		t.Fatal("stop missed the queue-started stream")
	}
	if err := <-stopped; !errors.Is(err, errStreamStopRequested) {
		t.Fatalf("cancel cause = %v", err)
	}
}

func TestAStoppedStreamConsumesNothing(t *testing.T) {
	var registry activeStreamRegistry
	stream, _ := registry.registerStream("u", "t", "s", func(error) {}, nil)
	registry.stop("u", "t", "s", errStreamStopRequested)
	called := false
	got, err := registry.consume(stream, func() ([]chat.Message, error) {
		called = true
		return []chat.Message{{ID: "m"}}, nil
	})
	if called || got != nil || err != nil {
		t.Fatalf("consume after stop: called=%v got=%v err=%v", called, got, err)
	}
}

func TestAFinishingStreamKeepsTheQueueUntilItHandsOff(t *testing.T) {
	var registry activeStreamRegistry
	stream, _ := registry.registerStream("u", "t", "s", func(error) {}, nil)
	registry.stop("u", "t", "s", errStreamStopRequested)
	insert := func() error { return nil }
	if started, _, _ := registry.enqueue("u", "t", insert, func(error) {}, nil); started != nil {
		t.Fatal("a stream started beside a stopped turn still unwinding")
	}
	handed := false
	registry.handOff(stream, func() bool { return true }, func() {
		handed = true
		registry.registerHandOff("u", "t", func(error) {}, nil)
	})
	if !handed {
		t.Fatal("the queue was not handed off")
	}
	// After the hand-off a new stream owns the thread: it reads the queue.
	if started, _, _ := registry.enqueue("u", "t", insert, func(error) {}, nil); started != nil {
		t.Fatal("enqueue must leave the queue to the handed-off stream")
	}
}

type recordingWriter struct{ names []string }

func (w *recordingWriter) SendEvents(events []sse.Event) error {
	for _, e := range events {
		w.names = append(w.names, e.Name)
	}
	return nil
}

func TestReattachReplaysFromTheLatestFollowUp(t *testing.T) {
	hub := newTurnHub()
	_ = hub.SendJSON("user_message", map[string]string{"id": "a"})
	_ = hub.SendJSON("assistant_message", map[string]string{"id": "b"})
	hub.startReplayHere()
	_ = hub.SendJSON("user_message", map[string]string{"id": "c"})
	hub.close()

	var reattached, sender recordingWriter
	hub.follow(context.Background(), &reattached, true)
	hub.follow(context.Background(), &sender, false)
	if strings.Join(reattached.names, ",") != "user_message" {
		t.Fatalf("reattach replayed %v, want only the follow-up", reattached.names)
	}
	if len(sender.names) != 3 {
		t.Fatalf("the sending client must keep the whole stream: %v", sender.names)
	}
}
