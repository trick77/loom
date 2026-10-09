// Package sse provides a minimal Server-Sent Events writer.
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Writer streams SSE events to an http.ResponseWriter. Send is safe for
// concurrent use: the main assistant loop and background workers (e.g.
// reasoning-title generation) may emit events at the same time.
type Writer struct {
	w            http.ResponseWriter
	rc           *http.ResponseController
	mu           sync.Mutex
	lastActivity time.Time
}

// writeTimeout bounds one event's write. A client that stops reading (a frozen
// phone tab keeps its TCP connection open) would otherwise block the write, and
// everything waiting on the stream, until the connection dies.
const writeTimeout = 30 * time.Second

// NewWriter sets SSE headers and returns a Writer, or an error if the
// ResponseWriter does not support flushing.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	if _, ok := w.(http.Flusher); !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Ask any fronting reverse proxy / LB that honors this convention (Traefik and
	// several others) not to buffer the stream.
	w.Header().Set("X-Accel-Buffering", "no")
	return &Writer{w: w, rc: http.NewResponseController(w), lastActivity: time.Now()}, nil
}

// Event is one named event with its data payload.
type Event struct {
	Name string
	Data string
}

// Send writes one event with the given name and data payload, then flushes.
func (s *Writer) Send(event, data string) error {
	return s.SendEvents([]Event{{Name: event, Data: data}})
}

// SendEvents writes events with a single flush. A client catching up on a
// long backlog then gets it in one go instead of one network write, and one
// render, per event.
func (s *Writer) SendEvents(events []Event) error {
	var b strings.Builder
	for _, e := range events {
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", e.Name, e.Data)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(b.String())
}

// writeLocked writes and flushes text under writeTimeout. The deadline is
// cleared afterwards: net/http does not reset it for the next request on a
// kept-alive connection. A writer without deadline support writes untimed.
func (s *Writer) writeLocked(text string) error {
	_ = s.rc.SetWriteDeadline(time.Now().Add(writeTimeout))
	defer func() { _ = s.rc.SetWriteDeadline(time.Time{}) }()
	if _, err := fmt.Fprint(s.w, text); err != nil {
		return err
	}
	if err := s.rc.Flush(); err != nil {
		return err
	}
	s.lastActivity = time.Now()
	return nil
}

// SendJSON writes one event whose data is data encoded as JSON. A value that
// does not encode returns the error and writes nothing.
func (s *Writer) SendJSON(event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return s.Send(event, string(payload))
}

// Heartbeat keeps the connection alive through idle proxies while the stream is
// silent. Some upstream models stream nothing to the client for tens of seconds
// while serializing a tool-call argument server-side; with no traffic
// an intermediary proxy, load balancer, or edge (e.g. Cloudflare's ~100s) may
// idle out the connection even though generation is progressing. A periodic SSE
// comment (": ...\n\n") is ignored by EventSource clients but resets those idle
// timers. The comment is only emitted when the stream has actually been quiet, so
// it adds no noise while events flow. Returns a stop function; call it (typically
// via defer) when the stream ends. Cancelling ctx also stops it.
func (s *Writer) Heartbeat(ctx context.Context, interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				s.mu.Lock()
				if time.Since(s.lastActivity) >= interval {
					// Best-effort: a write error here means the client is gone, which
					// the real Send path surfaces; don't disrupt the stream over a
					// failed keep-alive.
					_ = s.writeLocked(": keepalive\n\n")
				}
				s.mu.Unlock()
			}
		}
	}()
	var once sync.Once
	// stop is synchronous: it returns only after the goroutine has exited, so no
	// keep-alive can be written after the caller (e.g. an HTTP handler) returns.
	return func() {
		once.Do(func() { close(done) })
		<-stopped
	}
}
