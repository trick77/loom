package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trick77/loom/internal/auth"
	"github.com/trick77/loom/internal/chat"
)

func TestTurnHubWaitHoldsThenWakesOnAnEvent(t *testing.T) {
	hub := newTurnHub()
	got := make(chan int)
	go func() {
		events, next, done := hub.wait(context.Background(), 0, time.Minute, pollMaxBytes)
		if len(events) != 1 || done {
			t.Errorf("events = %v, done = %v", events, done)
		}
		got <- next
	}()
	time.Sleep(10 * time.Millisecond)
	_ = hub.SendJSON("thread", map[string]string{"id": "thr_1"})
	if next := <-got; next != 1 {
		t.Fatalf("next = %d, want 1", next)
	}
}

func TestTurnHubWaitAnswersEmptyAfterTheHold(t *testing.T) {
	hub := newTurnHub()
	_ = hub.SendJSON("thread", map[string]string{})
	events, next, done := hub.wait(context.Background(), 1, 10*time.Millisecond, pollMaxBytes)
	if len(events) != 0 || next != 1 || done {
		t.Fatalf("events = %v next = %d done = %v, want none at 1", events, next, done)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if events, next, _ := hub.wait(ctx, 1, time.Minute, pollMaxBytes); len(events) != 0 || next != 1 {
		t.Fatalf("a canceled poll = %v at %d", events, next)
	}
}

// A long backlog goes out in polls of about maxBytes, merged; the cursor stays
// a raw index, and the last poll of a closed hub is done.
func TestTurnHubWaitCapsMergesAndEnds(t *testing.T) {
	hub := newTurnHub()
	for i := range 10 {
		_ = hub.SendJSON("assistant_delta", deltaPayload{Content: fmt.Sprintf("%02d", i)})
	}
	_ = hub.SendJSON("done", struct{}{})
	hub.close()
	one := len(`assistant_delta`) + len(`{"content":"00"}`)
	var text strings.Builder
	from, polls := 0, 0
	for {
		events, next, done := hub.wait(context.Background(), from, time.Minute, 4*one)
		polls++
		for _, e := range events {
			if e.Name == "assistant_delta" {
				var p deltaPayload
				_ = json.Unmarshal([]byte(e.Data), &p)
				text.WriteString(p.Content)
			}
		}
		if next <= from {
			t.Fatalf("cursor stuck at %d", from)
		}
		from = next
		if done {
			break
		}
	}
	if text.String() != "00010203040506070809" || from != 11 || polls != 3 {
		t.Fatalf("text %q, cursor %d, polls %d", text.String(), from, polls)
	}
	// a cursor past the end is the end
	if _, next, done := hub.wait(context.Background(), 99, time.Minute, pollMaxBytes); next != 11 || !done {
		t.Fatalf("past the end: next %d done %v", next, done)
	}
}

func TestPollLookupFindsTheRunningTheRecentAndNothingElse(t *testing.T) {
	registry := activeStreamRegistry{keepRecent: true}
	now := time.Unix(0, 0)
	registry.now = func() time.Time { return now }
	first := newTurnHub()
	_, unregisterFirst := registry.registerStream("user_1", "thr_1", "send_1", func(error) {}, first)

	if hub, sendID, target := registry.pollLookup("user_1", "thr_1", ""); hub != first || sendID != "send_1" || target != pollFound {
		t.Fatalf("fresh poll = %v %q %v", hub, sendID, target)
	}
	if _, _, target := registry.pollLookup("user_2", "thr_1", ""); target != pollNone {
		t.Fatalf("another user's poll = %v, want none", target)
	}
	// A hand-off registers the next stream: the first stays pollable by id.
	second := newTurnHub()
	_, unregisterSecond := registry.registerHandOff("user_1", "thr_1", func(error) {}, second)
	unregisterFirst()
	if hub, _, target := registry.pollLookup("user_1", "thr_1", first.id); hub != first || target != pollFound {
		t.Fatalf("replaced hub = %v %v", hub, target)
	}
	if hub, sendID, _ := registry.pollLookup("user_1", "thr_1", ""); hub != second || sendID != "" {
		t.Fatalf("fresh poll after the hand-off = %v %q", hub, sendID)
	}
	if _, _, target := registry.pollLookup("user_1", "thr_1", "nope"); target != pollGone {
		t.Fatalf("unknown hub = %v, want gone", target)
	}
	// Ended: the last one stays pollable for recentWindow, then nothing runs.
	unregisterSecond()
	if hub, _, target := registry.pollLookup("user_1", "thr_1", second.id); hub != second || target != pollFound {
		t.Fatalf("ended hub = %v %v", hub, target)
	}
	now = now.Add(recentWindow)
	if _, _, target := registry.pollLookup("user_1", "thr_1", second.id); target != pollNone {
		t.Fatalf("expired hub = %v, want none", target)
	}
}

// What the registry remembers is bounded: a few hubs per thread, swept once
// they expire, dropped with the thread, and nothing at all for runs.
func TestRecentStreamsAreBounded(t *testing.T) {
	registry := activeStreamRegistry{keepRecent: true}
	now := time.Unix(0, 0)
	registry.now = func() time.Time { return now }
	var hubs []*turnHub
	for range maxRecent + 2 {
		hub := newTurnHub()
		hubs = append(hubs, hub)
		_, unregister := registry.registerStream("user_1", "thr_1", "", func(error) {}, hub)
		unregister()
	}
	if got := len(registry.recent[activeStreamKey{"user_1", "thr_1"}]); got != maxRecent {
		t.Fatalf("recent per thread = %d, want %d", got, maxRecent)
	}
	if _, _, target := registry.pollLookup("user_1", "thr_1", hubs[0].id); target != pollNone {
		t.Fatalf("oldest hub = %v, want dropped", target)
	}
	if hub, _, _ := registry.pollLookup("user_1", "thr_1", hubs[len(hubs)-1].id); hub != hubs[len(hubs)-1] {
		t.Fatal("newest hub dropped")
	}
	// another thread's end sweeps the expired ones
	now = now.Add(recentWindow)
	_, unregister := registry.registerStream("user_1", "thr_2", "", func(error) {}, newTurnHub())
	unregister()
	if _, kept := registry.recent[activeStreamKey{"user_1", "thr_1"}]; kept || len(registry.recent) != 1 {
		t.Fatalf("recent after the window = %v", registry.recent)
	}
	registry.forget("user_1", "thr_2")
	if len(registry.recent) != 0 {
		t.Fatalf("recent after forget = %v", registry.recent)
	}
	// a registry that keeps nothing
	var runs activeStreamRegistry
	_, unregister = runs.registerStream("user_1", "thr_1", "", func(error) {}, newTurnHub())
	unregister()
	if runs.recent != nil {
		t.Fatalf("runs kept %v", runs.recent)
	}
}

func newPollServer(t *testing.T) *server {
	t.Helper()
	deps := Deps{Thread: &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID}}, LLM: fakeChatClient{}}
	deps.Auth = auth.NewMiddleware(fakeSessionStore{userID: testUser.ID}, fakeUserStore{user: testUser, ok: true})
	s := newServer(deps)
	s.pollHold = 20 * time.Millisecond
	return s
}

func poll(s *server, query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := authenticatedRequest(http.MethodGet, "/api/threads/thr_1/messages:poll"+query, "")
	req.SetPathValue("threadID", "thr_1")
	s.requireAuth(http.HandlerFunc(s.handlePollStreamMessage)).ServeHTTP(rec, req)
	return rec
}

func TestPollAnswers(t *testing.T) {
	s := newPollServer(t)
	if rec := poll(s, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("no turn = %d, want 204", rec.Code)
	}
	hub := newTurnHub()
	_ = hub.SendJSON("user_message", map[string]string{"id": "m1"})
	hub.startReplayHere()
	_ = hub.SendJSON("assistant_delta", deltaPayload{Content: "Hi"})
	_, unregister := s.activeStreams.registerStream(testUser.ID, "thr_1", "send_1", func(error) {}, hub)

	// fresh: from replayFrom, with the hub and its send
	rec := poll(s, "")
	var got pollResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("fresh poll = %d %s", rec.Code, rec.Body)
	}
	if got.Hub != hub.id || got.SendID != "send_1" || got.Next != 2 || got.Done || len(got.Events) != 1 || got.Events[0].Event != "assistant_delta" {
		t.Fatalf("fresh poll = %+v", got)
	}
	// up to date: empty after the hold
	rec = poll(s, fmt.Sprintf("?hub=%s&after=2", hub.id))
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Next != 2 || len(got.Events) != 0 {
		t.Fatalf("held poll = %d %s", rec.Code, rec.Body)
	}
	// the end of an ended turn, still pollable by its id
	_ = hub.SendJSON("done", struct{}{})
	hub.close()
	unregister()
	rec = poll(s, fmt.Sprintf("?hub=%s&after=2", hub.id))
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || !got.Done || len(got.Events) != 1 || got.Events[0].Event != "done" {
		t.Fatalf("last poll = %d %s", rec.Code, rec.Body)
	}
	if rec := poll(s, "?hub=nope&after=0"); rec.Code != http.StatusNoContent {
		t.Fatalf("unknown hub, nothing running = %d, want 204", rec.Code)
	}
	_, unregister = s.activeStreams.registerStream(testUser.ID, "thr_1", "", func(error) {}, newTurnHub())
	defer unregister()
	if rec := poll(s, "?hub=nope&after=0"); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "hub_gone") {
		t.Fatalf("unknown hub = %d %s, want 409 hub_gone", rec.Code, rec.Body)
	}
	if rec := poll(s, fmt.Sprintf("?hub=%s&after=x", hub.id)); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor = %d, want 400", rec.Code)
	}
}

// Behind a proxy that buffers every response until it ends, the turn still
// arrives in pieces: each poll is a whole response, and the polls together
// carry what the stream carried.
func TestPollFollowsARunningTurnLikeTheStream(t *testing.T) {
	store := &fakeThreadStore{Thread: chat.Thread{ID: "thr_1", UserID: testUser.ID, Title: "Existing title"}}
	llmClient, release := newHeldChatClient()
	srv := newAuthenticatedServer(t, Deps{Thread: store, LLM: llmClient})

	sendRec := httptest.NewRecorder()
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		srv.ServeHTTP(sendRec, authenticatedRequest(http.MethodPost, "/api/threads/thr_1/messages:stream", `{"content":"Hi","clientMessageId":"send_1"}`))
	}()
	llmClient.waitStarted(t)

	var names []string
	query, polls := "", 0
	for {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authenticatedRequest(http.MethodGet, "/api/threads/thr_1/messages:poll"+query, ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("poll %d = %d %s", polls, rec.Code, rec.Body)
		}
		var got pollResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if polls == 0 {
			if got.SendID != "send_1" {
				t.Fatalf("first poll names send %q, want send_1", got.SendID)
			}
			close(release)
		}
		polls++
		for _, e := range got.Events {
			names = append(names, e.Event)
		}
		if got.Done {
			break
		}
		query = fmt.Sprintf("?hub=%s&after=%d", got.Hub, got.Next)
	}
	waitClosed(t, sendDone, "the stream handler")
	sent := collapseRepeats(sseEventNames(sendRec.Body.String()))
	if strings.Join(collapseRepeats(names), ",") != strings.Join(sent, ",") {
		t.Fatalf("polled events = %v, want the sent stream %v", names, sent)
	}
	if polls < 2 {
		t.Fatalf("polls = %d, want the turn in more than one", polls)
	}
}
