package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/trick77/loom/internal/sse"
)

// recordingEventWriter collects what a follower writes, and the size of each
// write; failAfter > 0 makes every write past that many fail, like a client
// that went away.
type recordingEventWriter struct {
	mu        sync.Mutex
	events    []string
	writes    []int
	failAfter int
}

func (w *recordingEventWriter) SendEvents(events []sse.Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failAfter > 0 && len(w.writes) >= w.failAfter {
		return errors.New("client gone")
	}
	for _, e := range events {
		w.events = append(w.events, e.Name+" "+e.Data)
	}
	w.writes = append(w.writes, len(events))
	return nil
}

func (w *recordingEventWriter) got() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.events, "|")
}

func TestTurnHubReplaysTheLogThenFollowsLive(t *testing.T) {
	hub := newTurnHub()
	_ = hub.SendJSON("user_message", map[string]string{"id": "m1"})
	_ = hub.SendJSON("delta", "Hel")

	w := &recordingEventWriter{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.follow(context.Background(), w, false)
	}()

	_ = hub.SendJSON("delta", "lo")
	_ = hub.SendJSON("done", struct{}{})
	hub.close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("follow did not return after close")
	}
	want := `user_message {"id":"m1"}|delta "Hel"|delta "lo"|done {}`
	if got := w.got(); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
	// The backlog is one write, so a reattaching client gets it at once.
	if w.writes[0] < 2 {
		t.Fatalf("first write carried %d events, want at least the 2-event backlog", w.writes[0])
	}
}

func TestTurnHubFollowAfterCloseReplaysEverything(t *testing.T) {
	hub := newTurnHub()
	_ = hub.SendJSON("delta", "a")
	hub.close()
	// A late event is dropped: the turn is over once the hub closed.
	_ = hub.SendJSON("delta", "b")

	w := &recordingEventWriter{}
	hub.follow(context.Background(), w, false)
	if got := w.got(); got != `delta "a"` {
		t.Fatalf("events = %s, want only the event before close", got)
	}
}

// A client that goes away must never fail the turn: the hub keeps accepting
// events and the follower simply stops.
func TestTurnHubFailedWriteStopsOnlyThatFollower(t *testing.T) {
	hub := newTurnHub()
	gone := &recordingEventWriter{failAfter: 1}
	live := &recordingEventWriter{}
	var wg sync.WaitGroup
	for _, w := range []*recordingEventWriter{gone, live} {
		wg.Go(func() { hub.follow(context.Background(), w, false) })
	}
	for _, delta := range []string{"a", "b", "c"} {
		if err := hub.SendJSON("delta", delta); err != nil {
			t.Fatalf("SendJSON() error = %v, want nil with a gone client", err)
		}
	}
	hub.close()
	wg.Wait()
	// The first write lands, the next fails, and the follower gives up.
	if len(gone.writes) != 1 {
		t.Fatalf("gone follower made %d writes, want 1 before it stopped", len(gone.writes))
	}
	if got := live.got(); got != `delta "a"|delta "b"|delta "c"` {
		t.Fatalf("live follower got %s, want every event", got)
	}
}

func TestTurnHubFollowStopsWhenItsContextEnds(t *testing.T) {
	hub := newTurnHub()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.follow(ctx, &recordingEventWriter{}, false)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("follow did not return after its context ended")
	}
}

// A replay merges consecutive deltas: a long turn is thousands of tiny events,
// and the client would otherwise dispatch every one of them.
func TestTurnHubReplayMergesConsecutiveDeltas(t *testing.T) {
	hub := newTurnHub()
	_ = hub.SendJSON("assistant_reasoning_delta", map[string]string{"content": "thin"})
	_ = hub.SendJSON("assistant_reasoning_delta", map[string]string{"content": "king"})
	_ = hub.SendJSON("assistant_delta", map[string]string{"content": "Hel"})
	_ = hub.SendJSON("assistant_delta", map[string]string{"content": "lo"})
	_ = hub.SendJSON("done", struct{}{})
	hub.close()

	w := &recordingEventWriter{}
	hub.follow(context.Background(), w, true)
	want := `assistant_reasoning_delta {"content":"thinking"}|assistant_delta {"content":"Hello"}|done {}`
	if got := w.got(); got != want {
		t.Fatalf("events = %s, want %s", got, want)
	}
}

// A backlog goes out in bounded writes, each under its own write deadline, so
// a slow link still gets through a long turn.
func TestTurnHubReplaySplitsALargeBacklog(t *testing.T) {
	hub := newTurnHub()
	chunk := strings.Repeat("x", maxReplayWriteBytes/2)
	for range 4 {
		_ = hub.SendJSON("tool_result", map[string]string{"output": chunk})
	}
	hub.close()

	w := &recordingEventWriter{}
	hub.follow(context.Background(), w, false)
	if len(w.writes) < 2 {
		t.Fatalf("backlog went out in %d write(s), want it split", len(w.writes))
	}
	if got := strings.Count(w.got(), "tool_result"); got != 4 {
		t.Fatalf("replayed %d events, want 4", got)
	}
}

func TestTurnHubSendJSONReportsAnUnencodableValue(t *testing.T) {
	if err := newTurnHub().SendJSON("delta", make(chan int)); err == nil {
		t.Fatal("SendJSON() error = nil, want the encoding error")
	}
}
