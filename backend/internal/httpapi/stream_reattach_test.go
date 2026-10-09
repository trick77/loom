package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/chat"
	"github.com/trick77/loom/internal/llm"
)

// heldChatClient streams a first reasoning delta, reports that the turn is
// running, then holds the answer until release closes (or the turn's context
// ends): a long answer the client walks away from mid-stream.
type heldChatClient struct {
	fakeChatClient
	started chan struct{}
}

func newHeldChatClient() (*heldChatClient, chan struct{}) {
	release := make(chan struct{})
	return &heldChatClient{
		fakeChatClient: fakeChatClient{ReasoningDeltas: []string{"thinking"}, ReasoningHold: release},
		started:        make(chan struct{}),
	}, release
}

func (f *heldChatClient) StreamChatWithTools(ctx context.Context, history []llm.Message, tools []llm.Tool, onEvent func(llm.StreamEvent) error) (llm.StreamResult, error) {
	close(f.started)
	return f.fakeChatClient.StreamChatWithTools(ctx, history, tools, onEvent)
}

func (f *heldChatClient) waitStarted(t *testing.T) {
	t.Helper()
	select {
	case <-f.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the turn never reached the model")
	}
}

func waitClosed(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not return", what)
	}
}

// firstWriteRecorder closes wrote on the first body write. It is read only
// after the handler returned.
type firstWriteRecorder struct {
	*httptest.ResponseRecorder
	wrote chan struct{}
	once  sync.Once
}

func (r *firstWriteRecorder) Write(p []byte) (int, error) {
	n, err := r.ResponseRecorder.Write(p)
	r.once.Do(func() { close(r.wrote) })
	return n, err
}

// sseEventNames lists the event names in an SSE body, in order, with
// consecutive repeats collapsed: a reattach merges runs of deltas.
func sseEventNames(body string) []string {
	var names []string
	for line := range strings.SplitSeq(body, "\n") {
		name, ok := strings.CutPrefix(line, "event: ")
		if ok && (len(names) == 0 || names[len(names)-1] != name) {
			names = append(names, name)
		}
	}
	return names
}

// A phone that freezes the tab drops the connection mid-answer. The turn must
// still finish and persist the whole answer, not just what streamed so far.
func TestStreamMessageTurnOutlivesTheClient(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient, release := newHeldChatClient()
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})

	reqCtx, dropClient := context.WithCancel(context.Background())
	defer dropClient()
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`).WithContext(reqCtx)
		srv.ServeHTTP(httptest.NewRecorder(), req)
	}()
	llmClient.waitStarted(t)
	dropClient()
	close(release)
	waitClosed(t, done, "the stream handler")

	if store.AssistantContent != "Hello" {
		t.Fatalf("assistant content = %q, want the full answer Hello", store.AssistantContent)
	}
}

// A client that comes back reattaches: it gets every event the turn sent so
// far, then the rest live, ending like the original stream.
func TestAttachReplaysTheRunningTurnThenFollowsIt(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient, release := newHeldChatClient()
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})

	sendRec := httptest.NewRecorder()
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		srv.ServeHTTP(sendRec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`))
	}()
	llmClient.waitStarted(t)

	attachRec := &firstWriteRecorder{ResponseRecorder: httptest.NewRecorder(), wrote: make(chan struct{})}
	attachDone := make(chan struct{})
	go func() {
		defer close(attachDone)
		srv.ServeHTTP(attachRec, authenticatedRequest(http.MethodGet, "/api/threads/thr_1/messages:attach", ""))
	}()
	// Release the answer only once the attach replays: a turn that ended before
	// the attach arrived would be a 204.
	waitClosed(t, attachRec.wrote, "the attach replay")
	close(release)
	waitClosed(t, sendDone, "the stream handler")
	waitClosed(t, attachDone, "the attach handler")

	if attachRec.Code != http.StatusOK {
		t.Fatalf("attach status = %d, want 200", attachRec.Code)
	}
	if got := attachRec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("attach content type = %q, want text/event-stream", got)
	}
	sent := sseEventNames(sendRec.Body.String())
	attached := sseEventNames(attachRec.Body.String())
	if strings.Join(attached, ",") != strings.Join(sent, ",") {
		t.Fatalf("attached events = %v, want the sent stream %v", attached, sent)
	}
	if len(attached) == 0 || attached[0] != "user_message" || attached[len(attached)-1] != "done" {
		t.Fatalf("attached events = %v, want user_message first and done last", attached)
	}
}

func TestAttachWithoutARunningTurnIsNoContent(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/threads/thr_1/messages:attach", ""))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

// The registry is keyed by user: another user's running turn on the same
// thread id is never found.
func TestActiveStreamRegistryLookupIsUserScoped(t *testing.T) {
	var registry activeStreamRegistry
	hub := newTurnHub()
	unregister := registry.register("user_1", "thr_1", func(error) {}, hub)
	if got := registry.lookup("user_2", "thr_1"); got != nil {
		t.Fatal("lookup found another user's turn")
	}
	if got := registry.lookup("user_1", "thr_1"); got != hub {
		t.Fatal("lookup did not find the owner's turn")
	}
	unregister()
	if got := registry.lookup("user_1", "thr_1"); got != nil {
		t.Fatal("lookup found a turn that has ended")
	}
}

// A reloaded page learns from the thread itself that an answer is still being
// written, and reattaches.
func TestGetThreadReportsARunningTurn(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient, release := newHeldChatClient()
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})

	getStreaming := func() bool {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/threads/thr_1", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("get thread status = %d: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Streaming bool `json:"streaming"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp.Streaming
	}
	if getStreaming() {
		t.Fatal("streaming = true before any turn")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(httptest.NewRecorder(), authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`))
	}()
	llmClient.waitStarted(t)
	if !getStreaming() {
		t.Fatal("streaming = false while the turn runs")
	}
	close(release)
	waitClosed(t, done, "the stream handler")
	if getStreaming() {
		t.Fatal("streaming = true after the turn ended")
	}
}

// Detached from the client, the turn must still stop when the server shuts
// down, so the database is not closed under it.
func TestStreamMessageTurnStopsWhenTheServerShutsDown(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient := &blockingChatClient{started: make(chan struct{}), done: make(chan struct{})}
	lifetime, shutDown := context.WithCancelCause(context.Background())
	defer shutDown(nil)
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient, Lifetime: lifetime})

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(httptest.NewRecorder(), authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`))
	}()
	select {
	case <-llmClient.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the turn never reached the model")
	}
	errShutdown := errors.New("server shutting down")
	shutDown(errShutdown)
	waitClosed(t, llmClient.done, "the model call")
	waitClosed(t, done, "the stream handler")
	if !errors.Is(llmClient.cancelCause, errShutdown) {
		t.Fatalf("cancel cause = %v, want the shutdown cause", llmClient.cancelCause)
	}
}

// A stop that arrives while the turn is still being set up finds nothing
// registered. It must still end that turn: the client's dropped fetch no
// longer does.
func TestActiveStreamRegistryAppliesAStopThatArrivedBeforeRegister(t *testing.T) {
	var registry activeStreamRegistry
	if registry.stop("user_1", "thr_1", errStreamStopRequested) {
		t.Fatal("stop() = true with nothing registered")
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	unregister := registry.register("user_1", "thr_1", cancel, nil)
	defer unregister()
	if !errors.Is(context.Cause(ctx), errStreamStopRequested) {
		t.Fatalf("cause = %v, want the early stop", context.Cause(ctx))
	}

	// Consumed once: the next turn on the thread runs.
	next, cancelNext := context.WithCancelCause(context.Background())
	defer cancelNext(nil)
	unregisterNext := registry.register("user_1", "thr_1", cancelNext, nil)
	defer unregisterNext()
	if next.Err() != nil {
		t.Fatal("a consumed early stop cancelled the next turn")
	}
}

func TestActiveStreamRegistryIgnoresAStaleEarlyStop(t *testing.T) {
	start := time.Unix(0, 0)
	registry := activeStreamRegistry{now: func() time.Time { return start }}
	registry.stop("user_1", "thr_1", errStreamStopRequested)
	registry.now = func() time.Time { return start.Add(earlyStopWindow + time.Second) }
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	unregister := registry.register("user_1", "thr_1", cancel, nil)
	defer unregister()
	if ctx.Err() != nil {
		t.Fatal("a stale early stop cancelled a later turn")
	}
}

// The handler must not hold the turn registered while the sending client's
// writes stall: the turn is over, and a delete waiting on it must not time out.
func TestStreamMessageUnregistersBeforeWaitingOnAStalledClient(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: fakeChatClient{}})

	rec := &stallingRecorder{ResponseRecorder: httptest.NewRecorder(), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi"}`))
	}()
	// The turn finishes on its own while the client is stuck on its first
	// write; from then on nothing is running on the thread.
	deadline := time.Now().Add(2 * time.Second)
	streaming := func() bool {
		r := httptest.NewRecorder()
		srv.ServeHTTP(r, authenticatedRequest(http.MethodGet, "/api/threads/thr_1/messages:attach", ""))
		return r.Code != http.StatusNoContent
	}
	for streaming() {
		if time.Now().After(deadline) {
			close(rec.release)
			t.Fatal("the finished turn stayed registered behind a stalled client")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(rec.release)
	waitClosed(t, done, "the stream handler")
	if store.AssistantContent != "Hello" {
		t.Fatalf("assistant content = %q, want the saved answer", store.AssistantContent)
	}
}

// stallingRecorder blocks every body write until release closes: a client
// whose connection is open but no longer reading.
type stallingRecorder struct {
	*httptest.ResponseRecorder
	release chan struct{}
}

func (r *stallingRecorder) Write(p []byte) (int, error) {
	<-r.release
	return r.ResponseRecorder.Write(p)
}
