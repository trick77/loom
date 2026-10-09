package httpapi

import (
	"context"
	"encoding/json"
	"sync"
)

// turnHub carries one chat turn's events to the clients watching it. The turn
// writes into the hub and never blocks on, or fails with, a client: the client
// that sent the message and any that reattach later each follow the hub's log
// at their own pace, and a client that goes away just stops following. A phone
// that freezes the tab mid-answer therefore no longer ends the turn, and on
// return it reattaches and gets the whole turn replayed from the start.
type turnHub struct {
	mu     sync.Mutex
	events []hubEvent
	closed bool
	// changed is closed and replaced on every append and on close, waking the
	// followers.
	changed chan struct{}
}

type hubEvent struct {
	name string
	data string
}

// eventWriter is the client end of a follower; *sse.Writer satisfies it.
type eventWriter interface {
	Send(event, data string) error
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
	h.events = append(h.events, hubEvent{name: event, data: string(payload)})
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

// follow writes the log to w from the start, then each event as it arrives,
// until the hub closes, ctx ends or a write fails.
func (h *turnHub) follow(ctx context.Context, w eventWriter) {
	next := 0
	for {
		h.mu.Lock()
		pending := h.events[next:]
		closed, changed := h.closed, h.changed
		h.mu.Unlock()
		for _, e := range pending {
			if err := w.Send(e.name, e.data); err != nil {
				return
			}
		}
		next += len(pending)
		if len(pending) > 0 {
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
