package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/trick77/loom/internal/sse"
)

// turnHub carries one chat turn's events to the clients watching it. The turn
// writes into the hub and never blocks on, or fails with, a client: the client
// that sent the message and any that reattach later each follow the hub's log
// at their own pace, and a client that goes away just stops following. A phone
// that freezes the tab mid-answer therefore no longer ends the turn, and on
// return it reattaches and gets the whole turn replayed from the start.
type turnHub struct {
	mu     sync.Mutex
	events []sse.Event
	closed bool
	// changed is closed and replaced on every append and on close, waking the
	// followers.
	changed chan struct{}
}

// eventWriter is the client end of a follower; *sse.Writer satisfies it.
type eventWriter interface {
	SendEvents(events []sse.Event) error
}

func newTurnHub() *turnHub {
	return &turnHub{changed: make(chan struct{})}
}

// SendJSON appends one event to the log. It fails only on a value that does
// not encode; an event after close is dropped.
func (h *turnHub) SendJSON(event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.events = append(h.events, sse.Event{Name: event, Data: string(payload)})
	close(h.changed)
	h.changed = make(chan struct{})
	return nil
}

// close ends the turn: followers drain the log and return.
func (h *turnHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.changed)
	}
}

// maxReplayWriteBytes bounds one write of a backlog. Each write runs under the
// sse write deadline, so a long turn replayed over a slow link must not be one
// write.
const maxReplayWriteBytes = 64 << 10

// mergedDeltaEvents are the events whose payloads concatenate: consecutive
// ones replay as one, so a reattaching client does not dispatch thousands.
var mergedDeltaEvents = map[string]bool{
	"assistant_delta":           true,
	"assistant_reasoning_delta": true,
}

// follow writes the log to w from the start, then each event as it arrives,
// until the hub closes, ctx ends or a write fails. Whatever piled up since the
// last write goes out together in bounded writes. A reattaching client
// (merge) also gets consecutive deltas merged, so its backlog lands at once;
// the sending client sees the deltas as the model produced them.
func (h *turnHub) follow(ctx context.Context, w eventWriter, merge bool) {
	next := 0
	for {
		h.mu.Lock()
		pending := h.events[next:]
		closed, changed := h.closed, h.changed
		h.mu.Unlock()
		if len(pending) > 0 {
			events := pending
			// A single live event has nothing to merge with.
			if merge && len(pending) > 1 {
				events = mergeDeltas(pending)
			}
			for _, batch := range splitEvents(events, maxReplayWriteBytes) {
				if err := w.SendEvents(batch); err != nil {
					return
				}
			}
			next += len(pending)
			continue
		}
		if closed {
			return
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return
		}
	}
}

// deltaPayload is the payload of the merged delta events.
type deltaPayload struct {
	Content string `json:"content"`
}

// mergeDeltas folds runs of the same delta event into one. Order is kept, and
// the client appends each delta to what it has, so the result is the same.
func mergeDeltas(events []sse.Event) []sse.Event {
	merged := make([]sse.Event, 0, len(events))
	var content strings.Builder
	flush := func(name string) {
		data, _ := json.Marshal(deltaPayload{Content: content.String()})
		merged = append(merged, sse.Event{Name: name, Data: string(data)})
		content.Reset()
	}
	run := ""
	for _, e := range events {
		var payload deltaPayload
		mergeable := mergedDeltaEvents[e.Name] && json.Unmarshal([]byte(e.Data), &payload) == nil
		if run != "" && (!mergeable || e.Name != run) {
			flush(run)
			run = ""
		}
		if !mergeable {
			merged = append(merged, e)
			continue
		}
		run = e.Name
		content.WriteString(payload.Content)
	}
	if run != "" {
		flush(run)
	}
	return merged
}

// splitEvents cuts events into batches of about maxBytes each; an event
// larger than that is a batch of its own.
func splitEvents(events []sse.Event, maxBytes int) [][]sse.Event {
	var batches [][]sse.Event
	start, size := 0, 0
	for i, e := range events {
		n := len(e.Name) + len(e.Data)
		if i > start && size+n > maxBytes {
			batches = append(batches, events[start:i])
			start, size = i, 0
		}
		size += n
	}
	if start < len(events) {
		batches = append(batches, events[start:])
	}
	return batches
}
