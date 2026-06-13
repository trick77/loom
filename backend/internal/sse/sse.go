// Package sse provides a minimal Server-Sent Events writer.
package sse

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Writer streams SSE events to an http.ResponseWriter. Send is safe for
// concurrent use: the main assistant loop and background workers (e.g.
// reasoning-title generation) may emit events at the same time.
type Writer struct {
	w            http.ResponseWriter
	flusher      http.Flusher
	mu           sync.Mutex
	lastActivity time.Time
}

// NewWriter sets SSE headers and returns a Writer, or an error if the
// ResponseWriter does not support flushing.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	return &Writer{w: w, flusher: flusher, lastActivity: time.Now()}, nil
}

// Send writes one event with the given name and data payload, then flushes.
func (s *Writer) Send(event, data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	s.flusher.Flush()
	s.lastActivity = time.Now()
	return nil
}

// Heartbeat keeps the connection alive through idle proxies while the stream is
// silent. Some upstream models (e.g. MiMo) stream nothing to the client for tens
// of seconds while serializing a tool-call argument server-side; with no traffic
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
					if _, err := fmt.Fprint(s.w, ": keepalive\n\n"); err == nil {
						s.flusher.Flush()
						s.lastActivity = time.Now()
					}
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
